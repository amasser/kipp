package kipp

// Store represents a data Store for kipp.
type Store interface {
	File(id string) (*File, error)
	Create(name string, size uint64, checksum string) (string, error)
}
