package main

import (
	"io"
	"os"
	"path/filepath"
)

const (
	logName    = "openlink-server.log"
	logMaxSize = 5 << 20 // rotated to openlink-server.log.1 at start-up past this size
)

// logDir is where openlink-server.log and diagnostics reports go: the config
// file's folder, else the program's.
func (c config) logDir() string {
	if c.path != "" {
		return filepath.Dir(c.path)
	}
	return exeDir()
}

// openLog appends to openlink-server.log in dir, so the log survives runs
// without a console (autostart). A log over logMaxSize is moved to .1 first.
func openLog(dir string) (io.WriteCloser, error) {
	path := filepath.Join(dir, logName)
	if fi, err := os.Stat(path); err == nil && fi.Size() > logMaxSize {
		os.Rename(path, path+".1")
	}
	return os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
}
