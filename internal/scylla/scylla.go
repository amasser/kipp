package scylla

import (
	"crypto/rand"
	"encoding/base64"
	"io"

	"github.com/gocql/gocql"
	"github.com/uhthomas/kipp/pkg/kipp"
)

const table = "kipp.files"

type Store struct {
	session                  *gocql.Session
	entityQuery, createQuery QueryFunc
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
	if err := s.Query(schema).Exec(); err != nil {
		return nil, err
	}
	return &Store{s, EntityQuery(s), CreateQuery(s, 0)}, nil
}

func (s *Store) Entity(id string) (*kipp.Entity, error) {
	var e kipp.Entity
	if err := s.entityQuery().Bind(id).Get(&e); err != nil {
		return nil, err
	}
	return &e, nil
}

func (s *Store) Create(name string, size uint64, checksum string) (string, error) {
	// 9 byte ID as base64 is most efficient when len(b) % 3 == 0
	var b [9]byte
	if _, err := io.ReadFull(rand.Reader, b[:]); err != nil {
		return "", err
	}
	id := base64.RawURLEncoding.EncodeToString(b[:])
	if err := s.createQuery().BindStruct(&kipp.Entity{
		ID:       id,
		Name:     name,
		Size:     size,
		Checksum: checksum,
	}).Exec(); err != nil {
		return "", err
	}
	return id, nil
}

func (s *Store) Close() error {
	s.session.Close()
	return nil
}
