package herd

import (
	"errors"
	filegenclient "github.com/Symantec/Dominator/lib/filegen/client"
	"github.com/Symantec/Dominator/lib/image"
	"github.com/Symantec/Dominator/lib/objectserver"
	"log"
	"runtime"
	"strconv"
	"syscall"
	"time"
)

func newHerd(imageServerAddress string, objectServer objectserver.ObjectServer,
	logger *log.Logger) *Herd {
	var herd Herd
	herd.imageServerAddress = imageServerAddress
	herd.objectServer = objectServer
	herd.computedFilesManager = filegenclient.New(objectServer, logger)
	herd.logger = logger
	herd.subsByName = make(map[string]*Sub)
	herd.imagesByName = make(map[string]*image.Image)
	herd.missingImages = make(map[string]missingImage)
	// Limit concurrent connection attempts so that the file descriptor limit is
	// not exceeded.
	var rlim syscall.Rlimit
	if err := syscall.Getrlimit(syscall.RLIMIT_NOFILE, &rlim); err != nil {
		panic(err)
	}
	maxConnAttempts := rlim.Cur - 50
	maxConnAttempts = (maxConnAttempts / 100)
	if maxConnAttempts < 1 {
		maxConnAttempts = 1
	} else {
		maxConnAttempts *= 100
	}
	herd.connectionSemaphore = make(chan struct{}, maxConnAttempts)
	herd.pollSemaphore = make(chan struct{}, runtime.NumCPU()*10)
	herd.pushSemaphore = make(chan struct{}, runtime.NumCPU())
	herd.computeSemaphore = make(chan struct{}, runtime.NumCPU())
	herd.currentScanStartTime = time.Now()
	return &herd
}

func (herd *Herd) pollNextSub() bool {
	if herd.nextSubToPoll >= uint(len(herd.subsByIndex)) {
		herd.nextSubToPoll = 0
		herd.previousScanDuration = time.Since(herd.currentScanStartTime)
		return true
	}
	if herd.nextSubToPoll == 0 {
		herd.currentScanStartTime = time.Now()
	}
	sub := herd.subsByIndex[herd.nextSubToPoll]
	herd.nextSubToPoll++
	if sub.busy { // Quick lockless check.
		return false
	}
	herd.connectionSemaphore <- struct{}{}
	go func() {
		defer func() { <-herd.connectionSemaphore }()
		if !sub.tryMakeBusy() {
			return
		}
		sub.connectAndPoll()
		sub.makeUnbusy()
	}()
	return false
}

func (herd *Herd) countSelectedSubs(selectFunc func(*Sub) bool) uint64 {
	herd.RLock()
	defer herd.RUnlock()
	if selectFunc == nil {
		return uint64(len(herd.subsByIndex))
	}
	count := 0
	for _, sub := range herd.subsByIndex {
		if selectFunc(sub) {
			count++
		}
	}
	return uint64(count)
}

func (herd *Herd) getSelectedSubs(selectFunc func(*Sub) bool) []*Sub {
	herd.RLock()
	defer herd.RUnlock()
	subs := make([]*Sub, 0, len(herd.subsByIndex))
	for _, sub := range herd.subsByIndex {
		if selectFunc == nil || selectFunc(sub) {
			subs = append(subs, sub)
		}
	}
	return subs
}

func (herd *Herd) getReachableSelector(query string) (func(*Sub) bool, error) {
	length := len(query)
	if length < 2 {
		return nil, errors.New("bad query")
	}
	var unit rDuration
	unitString := query[length-1]
	query = query[:length-1]
	switch unitString {
	case 's':
		unit = rDuration(time.Second)
	case 'm':
		unit = rDuration(time.Minute)
	case 'h':
		unit = rDuration(time.Hour)
	case 'd':
		unit = rDuration(time.Hour * 24)
	default:
		return nil, errors.New("unknown unit in query")
	}
	if val, err := strconv.ParseUint(query, 10, 64); err != nil {
		return nil, err
	} else {
		duration := rDuration(val) * unit
		return duration.selector, nil
	}
}
