package rpcd

import (
	"github.com/Symantec/Dominator/lib/objectserver"
	"github.com/Symantec/Dominator/lib/srpc"
	"github.com/Symantec/tricorder/go/tricorder"
	"github.com/Symantec/tricorder/go/tricorder/units"
	"io"
	"log"
)

type srpcType struct {
	objectServer objectserver.ObjectServer
	getSemaphore chan bool
	logger       *log.Logger
}

type htmlWriter struct {
	getSemaphore chan bool
}

func (hw *htmlWriter) WriteHtml(writer io.Writer) {
	hw.writeHtml(writer)
}

func Setup(objSrv objectserver.ObjectServer, logger *log.Logger) *htmlWriter {
	getSemaphore := make(chan bool, 100)
	srpcObj := &srpcType{objSrv, getSemaphore, logger}
	srpc.RegisterName("ObjectServer", srpcObj)
	tricorder.RegisterMetric("/get-requests",
		func() uint { return uint(len(getSemaphore)) },
		units.None, "number of GetObjects() requests in progress")
	return &htmlWriter{getSemaphore}
}
