package scylla

import (
	"crypto/rand"
	"encoding/base64"
	"io"
	"log"
	"os"
	"strings"

	"github.com/gocql/gocql"
	"github.com/uhthomas/kipp/pkg/kipp"
)

const table = "kipp.files"

type Store struct {
	session                *gocql.Session
	fileQuery, createQuery QueryFunc
}

// addresses
// query file/reader? at least the path?
func New(addr ...string) (*Store, error) {
	c := gocql.NewCluster(addr...)
	c.Keyspace = "kipp"
	s, err := c.CreateSession()
	if err != nil {
		return nil, err
	}
	f, err := os.Open("kipp.cql")
	if err != nil {
		return nil, err
	}
	var b strings.Builder
	if _, err := io.Copy(&b, f); err != nil {
		return nil, err
	}
	log.Print(b.String())
	if err := s.Query(b.String()).Exec(); err != nil {
		return nil, err
	}
	return &Store{s, NewFileQuery(s), NewCreateQuery(s, 0)}, nil
}

func (s *Store) File(id string) (*kipp.File, error) {
	var f kipp.File
	if err := s.fileQuery().Bind(id).GetRelease(&f); err != nil {
		return nil, err
	}
	return &f, nil
}

func (s *Store) Create(name string, size uint64, checksum string) (string, error) {
	// 9 byte ID as base64 is most efficient when len(b) % 3 == 0
	var b [9]byte
	if _, err := io.ReadFull(rand.Reader, b[:]); err != nil {
		return "", err
	}
	id := base64.RawURLEncoding.EncodeToString(b[:])
	if err := s.createQuery().BindStruct(&kipp.File{
		ID:       id,
		Name:     name,
		Size:     size,
		Checksum: checksum,
	}).ExecRelease(); err != nil {
		return "", err
	}
	return id, nil
}

func (s *Store) Close() error {
	s.session.Close()
	return nil
}
