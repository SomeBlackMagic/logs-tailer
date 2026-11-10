package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/prometheus/procfs"
	"io"
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
	processExistingFiles(*folderPath, processedFiles, os.Stdout)

	watcher := newFolderWatcher(*folderPath)
	defer func(watcher *fsnotify.Watcher) {
		err := watcher.Close()
		if err != nil {
			log.Fatalf("Can not close watcher: %v", err)
		}
	}(watcher)

	watchFolder(context.Background(), watcher, processedFiles, os.Stdout)

}

func processExistingFiles(folderPath string, processedFiles map[string]struct{}, out io.Writer) {
	filepath.Walk(folderPath, func(filePath string, info os.FileInfo, err error) error {
		if err != nil {
			log.Printf("Error while walking through files : %v", err)
			return err
		}
		if !info.IsDir() {
			go processFile(filePath, out)
			processedFiles[filePath] = struct{}{}
		}
		return nil
	})
}

// newFolderWatcher creates an fsnotify watcher subscribed to folderPath.
// The subscription is done synchronously so that no event is missed
// between this call and the start of watchFolder.
func newFolderWatcher(folderPath string) *fsnotify.Watcher {
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

	err = watcher.Add(folderPath)
	if err != nil {
		log.Fatalf("Error while adding folder for monitoring: %v", err)
	}

	return watcher
}

// watchFolder handles watcher events until ctx is cancelled or the watcher is closed.
func watchFolder(ctx context.Context, watcher *fsnotify.Watcher, processedFiles map[string]struct{}, out io.Writer) {
	for {
		select {
		case <-ctx.Done():
			return

		case event, ok := <-watcher.Events:
			if !ok {
				return
			}
			if event.Op&fsnotify.Create == fsnotify.Create {
				filePath := event.Name
				_, processed := processedFiles[filePath]
				if !processed {
					go processFile(filePath, out)
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

func processFile(filePath string, out io.Writer) {
	fmt.Fprintln(out, "New file created:", filePath)

	fileName := filepath.Base(filePath)

	// ReOpen is disabled on purpose: rotated files (e.g. deprecation-YYYY-MM-DD.log)
	// are deleted and never come back. With ReOpen the tail would wait forever for
	// the file to reappear, leaking this goroutine. Without it the tail stops on
	// delete/rename, closes its file descriptor and the Lines channel.
	//
	// MustExist makes TailFile open the file right away and fail if it is
	// already gone (short-lived file deleted before this goroutine ran).
	// Without it the first open is deferred and waits forever for the path.
	t, err := tail.TailFile(filePath, tail.Config{
		Follow:    true,
		ReOpen:    false,
		MustExist: true,
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
			fmt.Fprintln(out, err)
		} else {
			fmt.Fprintln(out, result)
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
