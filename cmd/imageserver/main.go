package main

import (
	"flag"
	"fmt"
	"github.com/Symantec/Dominator/imageserver/httpd"
	imageserverRpcd "github.com/Symantec/Dominator/imageserver/rpcd"
	"github.com/Symantec/Dominator/imageserver/scanner"
	"github.com/Symantec/Dominator/lib/constants"
	"github.com/Symantec/Dominator/lib/logbuf"
	"github.com/Symantec/Dominator/lib/objectserver/filesystem"
	objectserverRpcd "github.com/Symantec/Dominator/objectserver/rpcd"
	"github.com/Symantec/tricorder/go/tricorder"
	"github.com/Symantec/tricorder/go/tricorder/units"
	"log"
	"os"
)

var (
	archiveMode = flag.Bool("archiveMode", false,
		"If true, disable delete operations and require update server")
	caFile = flag.String("CAfile", "/etc/ssl/CA.pem",
		"Name of file containing the root of trust")
	certFile = flag.String("certFile", "/etc/ssl/imageserver/cert.pem",
		"Name of file containing the SSL certificate")
	debug    = flag.Bool("debug", false, "If true, show debugging output")
	imageDir = flag.String("imageDir", "/var/lib/imageserver",
		"Name of image server data directory.")
	imageServerHostname = flag.String("imageServerHostname", "",
		"Hostname of image server to receive updates from")
	imageServerPortNum = flag.Uint("imageServerPortNum",
		constants.ImageServerPortNumber,
		"Port number of image server")
	logbufLines = flag.Uint("logbufLines", 1024,
		"Number of lines to store in the log buffer")
	keyFile = flag.String("keyFile", "/etc/ssl/imageserver/key.pem",
		"Name of file containing the SSL key")
	objectDir = flag.String("objectDir", "/var/lib/objectserver",
		"Name of image server data directory.")
	portNum = flag.Uint("portNum", constants.ImageServerPortNumber,
		"Port number to allocate and listen on for HTTP/RPC")
)

type imageObjectServersType struct {
	imdb   *scanner.ImageDataBase
	objSrv *filesystem.ObjectServer
}

func main() {
	flag.Parse()
	tricorder.RegisterFlags()
	if os.Geteuid() == 0 {
		fmt.Fprintln(os.Stderr, "Do not run the Image Server as root")
		os.Exit(1)
	}
	if *archiveMode && *imageServerHostname == "" {
		fmt.Fprintln(os.Stderr, "-imageServerHostname required in archive mode")
		os.Exit(1)
	}
	setupTls(*caFile, *certFile, *keyFile)
	circularBuffer := logbuf.New(*logbufLines)
	logger := log.New(circularBuffer, "", log.LstdFlags)
	objSrv, err := filesystem.NewObjectServer(*objectDir, logger)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Cannot create ObjectServer\t%s\n", err)
		os.Exit(1)
	}
	imdb, err := scanner.LoadImageDataBase(*imageDir, objSrv, logger)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Cannot load image database\t%s\n", err)
		os.Exit(1)
	}
	tricorder.RegisterMetric("/image-count",
		func() uint { return imdb.CountImages() },
		units.None, "number of images")
	imgSrvRpcHtmlWriter := imageserverRpcd.Setup(imdb, *imageServerHostname,
		logger)
	objSrvRpcHtmlWriter := objectserverRpcd.Setup(objSrv, logger)
	httpd.AddHtmlWriter(imdb)
	httpd.AddHtmlWriter(&imageObjectServersType{imdb, objSrv})
	httpd.AddHtmlWriter(imgSrvRpcHtmlWriter)
	httpd.AddHtmlWriter(objSrvRpcHtmlWriter)
	httpd.AddHtmlWriter(circularBuffer)
	if *imageServerHostname != "" {
		go replicator(fmt.Sprintf("%s:%d", *imageServerHostname,
			*imageServerPortNum), imdb, objSrv, *archiveMode, logger)
	}
	if err = httpd.StartServer(*portNum, imdb, objSrv, false); err != nil {
		fmt.Fprintf(os.Stderr, "Unable to create http server\t%s\n", err)
		os.Exit(1)
	}
}
