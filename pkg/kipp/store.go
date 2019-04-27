package kipp

type Store interface {
	File(id string) File
	Create(name string, size uint64, checksum string) (string, error)
}
