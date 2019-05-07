package kipp

import "time"

// EntityCreator represents a data EntityCreator for kipp.
type EntityCreator interface {
	Entity(id string) (*Entity, error)
	Create(name string, size uint64, checksum string) (string, error)
}

// Entity represents information about an uploaded file.
type Entity struct {
	ID        string
	Checksum  string
	Name      string
	Size      uint64
	Lifetime  *time.Time
	Timestamp time.Time
}
