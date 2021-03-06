package rpcd

import (
	"encoding/gob"
	"github.com/Symantec/Dominator/lib/srpc"
	"github.com/Symantec/Dominator/proto/sub"
	"time"
)

var startTime time.Time = time.Now()

func (t *rpcType) Poll(conn *srpc.Conn) error {
	defer conn.Flush()
	var request sub.PollRequest
	var response sub.PollResponse
	decoder := gob.NewDecoder(conn)
	if err := decoder.Decode(&request); err != nil {
		_, err = conn.WriteString(err.Error() + "\n")
		return err
	}
	if _, err := conn.WriteString("\n"); err != nil {
		return err
	}
	response.NetworkSpeed = t.networkReaderContext.MaximumSpeed()
	response.CurrentConfiguration = t.getConfiguration()
	t.rwLock.RLock()
	response.FetchInProgress = t.fetchInProgress
	response.UpdateInProgress = t.updateInProgress
	if t.lastFetchError != nil {
		response.LastFetchError = t.lastFetchError.Error()
	}
	if !t.updateInProgress {
		if t.lastUpdateError != nil {
			response.LastUpdateError = t.lastUpdateError.Error()
		}
		response.LastUpdateHadTriggerFailures = t.lastUpdateHadTriggerFailures
	}
	t.rwLock.RUnlock()
	response.StartTime = startTime
	response.PollTime = time.Now()
	response.ScanCount = t.fileSystemHistory.ScanCount()
	response.GenerationCount = t.fileSystemHistory.GenerationCount()
	fs := t.fileSystemHistory.FileSystem()
	if fs != nil &&
		!request.ShortPollOnly &&
		request.HaveGeneration != t.fileSystemHistory.GenerationCount() {
		response.FileSystemFollows = true
	}
	encoder := gob.NewEncoder(conn)
	if err := encoder.Encode(response); err != nil {
		return err
	}
	if response.FileSystemFollows {
		if err := fs.FileSystem.Encode(conn); err != nil {
			return err
		}
		if err := fs.ObjectCache.Encode(conn); err != nil {
			return err
		}
	}
	return nil
}
