package main

import (
    "flag"
    "fmt"
    "log"
    "os"
    "path/filepath"
    "sync"

    "github.com/fsnotify/fsnotify"
    "github.com/hpcloud/tail"
)

var version = "development"
var revision = "000000000000000000000000000000"

func main() {
    fmt.Printf("Starting Logs Tailer Version: %s\n", version)

    folderPath := flag.String("folder", ".", "Path to files folder")
    flag.Parse()

    processedFiles := make(map[string]struct{})
    var wg sync.WaitGroup

    // Processing exist files
    processExistingFiles(*folderPath, processedFiles)

    wg.Add(1)
    go func() {
        defer wg.Done()
        watchFolder(*folderPath, processedFiles)
    }()

    wg.Wait()
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
        log.Fatalf("Error while creating watcher: %v", err)
    }
    defer watcher.Close()

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
        fmt.Println("Can not open file:", err)
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

    if err != nil {
        log.Printf("Error to create log journal: %v", err)
        return
    }

    for line := range t.Lines {
        fmt.Println(fileName + ":" + line.Text)
    }
}
