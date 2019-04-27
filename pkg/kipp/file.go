package kipp

import "time"

type File struct {
	ID        string
	Checksum  string
	Name      string
	Size      uint64
	Expires   *time.Time
	Timestamp time.Time
}
