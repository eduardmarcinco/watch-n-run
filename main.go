package main

import (
	"flag"
	"fmt"
	"github.com/fsnotify/fsnotify"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

var watcher *fsnotify.Watcher

var (
	server string
	username string
	password string

	shellScript string
	rootPath  string
	delay     int

	ignoreArg string
	ignores   []string
)

func init() {
	flag.StringVar(&server, "server", "", "Specifies the VM UUID or VM name")
	flag.StringVar(&username, "username", "", "Specifies the user name on guest OS under which the process should run. This user name must already exist on the guest OS.")
	flag.StringVar(&password, "password", "", "Password for given user name")

	flag.StringVar(&shellScript, "shellScript", "", "Path to the shell script that will be executed when filesystem change occurs")
	flag.StringVar(&rootPath, "path", "", "Root path where to watch for changes")
	flag.IntVar(&delay, "delay", 100, "Delay in milliseconds before notifying about a file that's changed")
	flag.StringVar(&ignoreArg, "ignore", "node_modules;.git;.idea", "Semicolon-separated list of directories to ignore. "+
		"Glob expressions are supported.")
}

func main() {
	flag.Parse()

	ignores = strings.Split(ignoreArg, ";")

	watcher, _ = fsnotify.NewWatcher()
	defer watcher.Close()
	if rootPath == "" {
		rootPath = "."
	}

	if err := filepath.Walk(rootPath, watchDir); err != nil {
		fmt.Println("ERROR", err)
	}

	// Map of filenames we're currently notifying about.
	var processes sync.Map

	for {
		select {
		case event := <-watcher.Events:
			switch event.Op {
			case fsnotify.Write:
				if _, ok := processes.Load(event.Name); ok {
					continue
				}
				processes.Store(event.Name, nil)

				go func(event fsnotify.Event) {
					defer processes.Delete(event.Name)

					// Wait for further events to accomodate the way editors
					// save files.
					time.Sleep(time.Duration(delay) * time.Millisecond)

					// Ensure the file hasn't been renamed or removed.
					if _, ok := processes.Load(event.Name); !ok {
						return
					}

					notifyVM(event)
				}(event)

			case fsnotify.Rename, fsnotify.Remove:
				processes.Delete(event.Name)
			}
		case err := <-watcher.Errors:
			fmt.Println("Error: ", err)
		}
	}
}

func notifyVM(event fsnotify.Event) {
	if event.Op != fsnotify.Write {
		return
	}

	var str strings.Builder

	str.WriteString("--nologo guestcontrol ")
	str.WriteString(server)
	str.WriteString(" run --exe /bin/bash --username ")
	str.WriteString(username)
	str.WriteString(" --password ")
	str.WriteString(password)
	str.WriteString(" --wait-stdout --wait-stderr --unquoted-args -- bash/arg0 ")
	str.WriteString( shellScript )

	args := strings.Fields( str.String() )

	cmd := exec.Command("VBoxManage", args[0:]...)

	stdoutStderr, err := cmd.CombinedOutput()
	if err != nil {
		fmt.Printf("Error: %s\n", err)
	}
	fmt.Printf("%s\n", stdoutStderr)
}

func watchDir(path string, fi os.FileInfo, err error) error {
	if !fi.Mode().IsDir() {
		return nil
	}

	// Ignore hidden directories.
	if len(path) > 1 && strings.HasPrefix(path, ".") {
		return filepath.SkipDir
	}

	for _, pattern := range ignores {
		ok, err := filepath.Match(pattern, fi.Name())
		if err != nil {
			return err
		}
		if ok {
			return filepath.SkipDir
		}
	}

	fmt.Println("Watching ", path)
	return watcher.Add(path)
}