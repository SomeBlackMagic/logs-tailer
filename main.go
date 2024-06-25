package main

import (
	"flag"
	"fmt"
	"github.com/prometheus/procfs"
	"log"
	"os"
	"path/filepath"

	"github.com/fsnotify/fsnotify"
	"github.com/hpcloud/tail"

	_ "github.com/prometheus/procfs"
	_ "net/http"
	_ "net/http/pprof"
)

var version = ""
var revision = "000000000000000000000000000000"

func main() {
	fmt.Printf("Starting Logs Tailer Version: %s\n", version)

	//r := http.NewServeMux()
	//
	//r.HandleFunc("/debug/pprof/", pprof.Index)
	//r.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	//r.HandleFunc("/debug/pprof/profile", pprof.Profile)
	//r.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	//r.HandleFunc("/debug/pprof/trace", pprof.Trace)
	//
	//http.ListenAndServe(":8080", r)

	folderPath := flag.String("folder", ".", "Path to files folder")
	flag.Parse()

	processedFiles := make(map[string]struct{})

	// Processing exist files
	processExistingFiles(*folderPath, processedFiles)

	watchFolder(*folderPath, processedFiles)

}

func processExistingFiles(folderPath string, processedFiles map[string]struct{}) {
	filepath.Walk(folderPath, func(filePath string, info os.FileInfo, err error) error {
		if err != nil {
			log.Printf("Error while walking through files : %v", err)
			return err
		}
		if !info.IsDir() {
			go processFile(filePath)
			processedFiles[filePath] = struct{}{}
		}
		return nil
	})
}

func watchFolder(folderPath string, processedFiles map[string]struct{}) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		p1, err := procfs.Self()
		if err != nil {
			log.Fatalf("could not get process: %s", err)
		}

		fdinfos, err := p1.FileDescriptorsInfo()
		if err != nil {
			log.Fatal(err)
		}
		l, err := fdinfos.InotifyWatchLen()
		if err != nil {
			log.Fatal(err)
		}
		log.Printf("Watchers %v", l)

		log.Fatalf("Error while creating watcher: %v", err)
	}
	defer func(watcher *fsnotify.Watcher) {
		err := watcher.Close()
		if err != nil {
			log.Fatalf("Can not close watcher: %v", err)
		}
	}(watcher)

	err = watcher.Add(folderPath)
	if err != nil {
		log.Fatalf("Error while adding folder for monitoring: %v", err)
	}

	for {
		select {
		case event, ok := <-watcher.Events:
			if !ok {
				return
			}
			if event.Op&fsnotify.Create == fsnotify.Create {
				filePath := event.Name
				_, processed := processedFiles[filePath]
				if !processed {
					go processFile(filePath)
					processedFiles[filePath] = struct{}{}
				}
			}

		case err, ok := <-watcher.Errors:
			if !ok {
				return
			}
			log.Printf("Error in monitoring: %v", err)
		}
	}
}

func processFile(filePath string) {
	fmt.Println("New file created:", filePath)
	file, err := os.Open(filePath)
	if err != nil {
		fmt.Println("Can not open file: ", err)
		return
	}
	defer file.Close()

	fileName := filepath.Base(filePath)

	t, err := tail.TailFile(filePath, tail.Config{
		Follow:    true,
		ReOpen:    true,
		MustExist: false,
		Poll:      true,
	})
	defer t.Cleanup()

	if err != nil {
		log.Printf("Error to create log journal: %v", err)
		return
	}

	for line := range t.Lines {
		fmt.Println(fileName + ":" + line.Text)
	}

}
