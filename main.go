package main

import (
	"bytes"
	"encoding/json"
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
		p1, errProc := procfs.Self()
		if errProc != nil {
			log.Fatalf("could not get process: %s", errProc)
		}

		fdinfos, errFileDescriptorsInfo := p1.FileDescriptorsInfo()
		if errFileDescriptorsInfo != nil {
			log.Fatal(errFileDescriptorsInfo)
		}
		l, errInotifyWatchLen := fdinfos.InotifyWatchLen()
		if errInotifyWatchLen != nil {
			log.Fatal("InotifyWatchLen error: ", errInotifyWatchLen)
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
			// Forget removed/renamed files so a file re-created with the same
			// name is picked up again.
			if event.Op&(fsnotify.Remove|fsnotify.Rename) != 0 {
				delete(processedFiles, event.Name)
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

	fileName := filepath.Base(filePath)

	// ReOpen is disabled on purpose: rotated files (e.g. deprecation-YYYY-MM-DD.log)
	// are deleted and never come back. With ReOpen the tail would wait forever for
	// the file to reappear, leaking this goroutine. Without it the tail stops on
	// delete/rename, closes its file descriptor and the Lines channel.
	t, err := tail.TailFile(filePath, tail.Config{
		Follow:    true,
		ReOpen:    false,
		MustExist: false,
		Poll:      true,
	})
	if err != nil {
		log.Printf("Error to create log journal: %v", err)
		return
	}
	defer t.Cleanup()

	for line := range t.Lines {
		result, err := processJSON(line.Text, fileName)
		if err != nil {
			fmt.Println(err)
		} else {
			fmt.Println(result)
		}

	}

}

func processJSON(input string, file string) (string, error) {
	// Verify if input is valid JSON

	// Parse input JSON into a map to access keys and values
	var jsonData map[string]json.RawMessage
	if err := json.Unmarshal([]byte(input), &jsonData); err != nil {
		return file + ":" + input, nil
	}

	// Prepare ordered JSON
	var buffer bytes.Buffer
	buffer.WriteString("{")
	buffer.WriteString(fmt.Sprintf(`"file":%q`, file))
	buffer.WriteString(",")

	first := true
	for key, value := range jsonData {
		if !first {
			buffer.WriteString(",")
		}
		first = false
		buffer.WriteString(fmt.Sprintf(`%q:%s`, key, value))
	}

	// Append the new field "file" at the end

	buffer.WriteString("}")

	return buffer.String(), nil
}
