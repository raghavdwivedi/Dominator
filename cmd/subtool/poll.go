package main

import (
	"encoding/gob"
	"fmt"
	"github.com/Symantec/Dominator/lib/srpc"
	"github.com/Symantec/Dominator/proto/sub"
	"github.com/Symantec/Dominator/sub/client"
	"os"
	"time"
)

func pollSubcommand(srpcClient *srpc.Client, args []string) {
	var err error
	clientName := fmt.Sprintf("%s:%d", *subHostname, *subPortNum)
	for iter := 0; *numPolls < 0 || iter < *numPolls; iter++ {
		if iter > 0 {
			time.Sleep(time.Duration(*interval) * time.Second)
		}
		if srpcClient == nil {
			srpcClient, err = srpc.DialHTTP("tcp", clientName, 0)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error dialing\t%s\n", err)
				os.Exit(1)
			}
		}
		var request sub.PollRequest
		var reply sub.PollResponse
		pollStartTime := time.Now()
		err = client.CallPoll(srpcClient, request, &reply)
		fmt.Printf("Poll duration: %s\n", time.Since(pollStartTime))
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error calling\t%s\n", err)
			os.Exit(1)
		}
		if *newConnection {
			srpcClient.Close()
			srpcClient = nil
		}
		fs := reply.FileSystem
		if fs == nil {
			fmt.Println("No FileSystem pointer")
		} else {
			fs.RebuildInodePointers()
			if *debug {
				fs.List(os.Stdout)
			} else {
				fmt.Println(fs)
			}
			fmt.Printf("Num objects: %d\n", len(reply.ObjectCache))
			if *file != "" {
				f, err := os.Create(*file)
				if err != nil {
					fmt.Fprintf(os.Stderr, "Error creating: %s\t%s\n",
						*file, err)
					os.Exit(1)
				}
				encoder := gob.NewEncoder(f)
				encoder.Encode(fs)
				f.Close()
			}
		}
	}
	time.Sleep(time.Duration(*wait) * time.Second)
}
