package memory

import (
	"github.com/Symantec/Dominator/lib/hash"
)

func (objSrv *ObjectServer) listObjects() []hash.Hash {
	objSrv.rwLock.RLock()
	defer objSrv.rwLock.RUnlock()
	hashes := make([]hash.Hash, 0, len(objSrv.objectMap))
	for hashVal := range objSrv.objectMap {
		hashes = append(hashes, hashVal)
	}
	return hashes
}
