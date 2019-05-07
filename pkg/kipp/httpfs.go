package kipp

import (
	"net/http"
	"os"
	"time"
)

// httpFileSystemFunc implements http.FileSystem.
type httpFileSystemFunc func(string) (http.File, error)

func (f httpFileSystemFunc) Open(name string) (http.File, error) { return f(name) }

// httpFile wraps http.File to provide correct Last-Modified times.
type httpFile struct {
	http.File
	modTime time.Time
}

func (f httpFile) Stat() (os.FileInfo, error) {
	d, err := f.File.Stat()
	if err == nil {
		d = fileInfo{d, f.modTime}
	}
	return d, err
}

type fileInfo struct {
	os.FileInfo
	modTime time.Time
}

func (d fileInfo) ModTime() time.Time { return d.modTime }
