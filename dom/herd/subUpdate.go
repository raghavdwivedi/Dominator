package herd

import (
	"fmt"
	"github.com/Symantec/Dominator/lib/filesystem"
	"github.com/Symantec/Dominator/lib/filter"
	subproto "github.com/Symantec/Dominator/proto/sub"
	"path"
	"syscall"
	"time"
)

type state struct {
	subFS                   *filesystem.FileSystem
	requiredFS              *filesystem.FileSystem
	requiredInodeToSubInode map[uint64]uint64
	inodesChanged           map[uint64]bool   // Required inode number.
	inodesCreated           map[uint64]string // Required inode number.
	subFilenameToInode      map[string]uint64
}

func (sub *Sub) buildUpdateRequest(request *subproto.UpdateRequest) {
	fmt.Println("buildUpdateRequest()") // TODO(rgooch): Delete debugging.
	var state state
	state.subFS = &sub.fileSystem.FileSystem
	requiredImage := sub.herd.getImage(sub.requiredImage)
	state.requiredFS = requiredImage.FileSystem
	filter := requiredImage.Filter
	request.Triggers = requiredImage.Triggers
	state.requiredInodeToSubInode = make(map[uint64]uint64)
	state.inodesChanged = make(map[uint64]bool)
	state.inodesCreated = make(map[uint64]string)
	var rusageStart, rusageStop syscall.Rusage
	syscall.Getrusage(syscall.RUSAGE_SELF, &rusageStart)
	compareDirectories(request, &state,
		&state.subFS.DirectoryInode, &state.requiredFS.DirectoryInode,
		"/", filter)
	syscall.Getrusage(syscall.RUSAGE_SELF, &rusageStop) // HACK
	cpuTime := time.Duration(rusageStop.Utime.Sec)*time.Second +
		time.Duration(rusageStop.Utime.Usec)*time.Microsecond -
		time.Duration(rusageStart.Utime.Sec)*time.Second -
		time.Duration(rusageStart.Utime.Usec)*time.Microsecond
	fmt.Printf("Build update request took: %s user CPU time\n", cpuTime)
}

func compareDirectories(request *subproto.UpdateRequest, state *state,
	subDirectory, requiredDirectory *filesystem.DirectoryInode,
	myPathName string, filter *filter.Filter) {
	// First look for entries that should be deleted.
	if subDirectory != nil {
		for name := range subDirectory.EntriesByName {
			pathname := path.Join(myPathName, name)
			if filter.Match(pathname) {
				continue
			}
			if _, ok := requiredDirectory.EntriesByName[name]; !ok {
				request.PathsToDelete = append(request.PathsToDelete, pathname)
				fmt.Printf("Delete: %s\n", pathname) // HACK
			}
		}
	}
	for name, requiredEntry := range requiredDirectory.EntriesByName {
		pathname := path.Join(myPathName, name)
		if filter.Match(pathname) {
			continue
		}
		var subEntry *filesystem.DirectoryEntry
		if subDirectory != nil {
			if se, ok := subDirectory.EntriesByName[name]; ok {
				subEntry = se
			}
		}
		if subEntry == nil {
			addEntry(request, state, requiredEntry, pathname)
		} else {
			compareEntries(request, state, subEntry, requiredEntry, pathname,
				filter)
		}
		// If a directory: descend (possibly with the directory for the sub).
		requiredInode := requiredEntry.Inode()
		if requiredInode, ok := requiredInode.(*filesystem.DirectoryInode); ok {
			var subInode *filesystem.DirectoryInode
			if subEntry != nil {
				if si, ok := subEntry.Inode().(*filesystem.DirectoryInode); ok {
					subInode = si
				}
			}
			compareDirectories(request, state, subInode, requiredInode,
				pathname, filter)
		}
	}
}

func addEntry(request *subproto.UpdateRequest, state *state,
	requiredEntry *filesystem.DirectoryEntry, myPathName string) {
	requiredInode := requiredEntry.Inode()
	if requiredInode, ok := requiredInode.(*filesystem.DirectoryInode); ok {
		makeDirectory(request, requiredInode, myPathName, true)
	} else {
		addInode(request, state, requiredEntry, myPathName)
	}
}

func compareEntries(request *subproto.UpdateRequest, state *state,
	subEntry, requiredEntry *filesystem.DirectoryEntry,
	myPathName string, filter *filter.Filter) {
	subInode := subEntry.Inode()
	requiredInode := requiredEntry.Inode()
	sameType, sameMetadata, sameData := filesystem.CompareInodes(
		subInode, requiredInode, nil)
	if requiredInode, ok := requiredInode.(*filesystem.DirectoryInode); ok {
		if sameMetadata {
			return
		}
		if sameType {
			makeDirectory(request, requiredInode, myPathName, false)
		} else {
			request.PathsToDelete = append(request.PathsToDelete, myPathName)
			makeDirectory(request, requiredInode, myPathName, true)
			fmt.Printf("Replace non-directory: %s...\n", myPathName) // HACK
		}
		return
	}
	if sameType && sameData && sameMetadata {
		relink(request, state, subEntry, requiredEntry, myPathName)
		return
	}
	if sameType && sameData {
		updateMetadata(request, state, requiredEntry, myPathName)
		relink(request, state, subEntry, requiredEntry, myPathName)
		return
	}
	request.PathsToDelete = append(request.PathsToDelete, myPathName)
	addInode(request, state, requiredEntry, myPathName)
}

func relink(request *subproto.UpdateRequest, state *state,
	subEntry, requiredEntry *filesystem.DirectoryEntry, myPathName string) {
	subInum, ok := state.requiredInodeToSubInode[requiredEntry.InodeNumber]
	if !ok {
		state.requiredInodeToSubInode[requiredEntry.InodeNumber] =
			subEntry.InodeNumber
		return
	}
	if subInum == subEntry.InodeNumber {
		return
	}
	makeHardlink(request,
		myPathName, state.subFS.InodeToFilenamesTable[subInum][0])
}

func makeHardlink(request *subproto.UpdateRequest, source, target string) {
	var hardlink subproto.Hardlink
	hardlink.Source = source
	hardlink.Target = target
	request.HardlinksToMake = append(request.HardlinksToMake, hardlink)
	fmt.Printf("Make link: %s => %s\n", source, target) // HACK
}

func updateMetadata(request *subproto.UpdateRequest, state *state,
	requiredEntry *filesystem.DirectoryEntry, myPathName string) {
	if state.inodesChanged[requiredEntry.InodeNumber] {
		return
	}
	var inode subproto.Inode
	inode.Name = myPathName
	inode.GenericInode = requiredEntry.Inode()
	request.InodesToChange = append(request.InodesToChange, inode)
	state.inodesChanged[requiredEntry.InodeNumber] = true
	fmt.Printf("Update metadata: %s\n", myPathName) // HACK
}

func makeDirectory(request *subproto.UpdateRequest,
	requiredInode *filesystem.DirectoryInode, pathName string, create bool) {
	var newdir subproto.Directory
	newdir.Name = pathName
	newdir.Mode = requiredInode.Mode
	newdir.Uid = requiredInode.Uid
	newdir.Gid = requiredInode.Gid
	if create {
		request.DirectoriesToMake = append(request.DirectoriesToMake, newdir)
		fmt.Printf("Add directory: %s...\n", pathName) // HACK
	} else {
		request.DirectoriesToChange = append(request.DirectoriesToMake, newdir)
		fmt.Printf("Change directory: %s...\n", pathName) // HACK
	}
}

func addInode(request *subproto.UpdateRequest, state *state,
	requiredEntry *filesystem.DirectoryEntry, myPathName string) {
	requiredInode := requiredEntry.Inode()
	if name, ok := state.inodesCreated[requiredEntry.InodeNumber]; ok {
		makeHardlink(request, myPathName, name)
		return
	}
	// Try to find a sibling inode.
	names := state.requiredFS.InodeToFilenamesTable[requiredEntry.InodeNumber]
	if len(names) > 1 {
		var sameDataInode filesystem.GenericInode
		var sameDataName string
		for _, name := range names {
			if inum, found := state.getSubInodeFromFilename(name); found {
				subInode := state.subFS.InodeTable[inum]
				_, sameMetadata, sameData := filesystem.CompareInodes(
					subInode, requiredInode, nil)
				if sameMetadata && sameData {
					makeHardlink(request, myPathName, name)
					return
				}
				if sameData {
					sameDataInode = subInode
					sameDataName = name
				}
			}
		}
		if sameDataInode != nil {
			updateMetadata(request, state, requiredEntry, sameDataName)
			makeHardlink(request, myPathName, sameDataName)
			return
		}
	}
	var inode subproto.Inode
	inode.Name = myPathName
	inode.GenericInode = requiredEntry.Inode()
	request.InodesToMake = append(request.InodesToMake, inode)
	state.inodesCreated[requiredEntry.InodeNumber] = myPathName
	fmt.Printf("Add entry: %s...\n", myPathName) // HACK
	// TODO(rgooch): Add entry.
}

func (state *state) getSubInodeFromFilename(name string) (uint64, bool) {
	if state.subFilenameToInode == nil {
		fmt.Println("Making subFilenameToInode map...") // HACK
		state.subFilenameToInode = make(map[string]uint64)
		for inum, names := range state.subFS.InodeToFilenamesTable {
			for _, n := range names {
				state.subFilenameToInode[n] = inum
			}
		}
		fmt.Println("Made subFilenameToInode map") // HACK
	}
	inum, ok := state.subFilenameToInode[name]
	return inum, ok
}
