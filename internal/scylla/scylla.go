package scylla

import (
	"crypto/rand"
	"encoding/base64"
	"io"
	"log"
	"os"
	"strings"
	"time"

	"github.com/gocql/gocql"
	"github.com/scylladb/gocqlx"
	scyllaqb "github.com/scylladb/gocqlx/qb"
	"github.com/uhthomas/kipp/pkg/kipp"
)

const table = "kipp.files"

type Store struct {
	session  *gocql.Session
	lifetime time.Duration
}

func NewStore(addr ...string) (*Store, error) {
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
	return &Store{s, 0}, err
}

func (s *Store) File(id string) (*kipp.File, error) {
	return nil, nil
}

func (s *Store) Create(name string, size uint64, checksum string) (string, error) {
	// 9 byte ID as base64 is most efficient when len(b) % 3 == 0
	var b [9]byte
	if _, err := io.ReadFull(rand.Reader, b[:]); err != nil {
		return "", err
	}
	id := base64.RawURLEncoding.EncodeToString(b[:])
	stmt, names := scyllaqb.
		Insert(table).
		Columns("id", "name", "size", "checksum").
		TTL(s.lifetime).
		ToCql()
	if err := gocqlx.Query(s.session.Query(stmt), names).BindStruct(&kipp.File{
		ID:       id,
		Name:     name,
		Size:     size,
		Checksum: checksum,
	}).ExecRelease(); err != nil {
		return "", err
	}
	return id, nil
}
