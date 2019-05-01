package scylla

import (
	"time"

	"github.com/gocql/gocql"
	"github.com/scylladb/gocqlx"
	scyllaqb "github.com/scylladb/gocqlx/qb"
)

type QueryFunc func() *gocqlx.Queryx

func NewFileQuery(s *gocql.Session) QueryFunc {
	stmt, names := scyllaqb.
		Select(table).
		Columns("id", "name", "size", "checksum", "timestamp", "TTL(id)").
		Where(scyllaqb.Eq("id")).
		Limit(1).
		ToCql()
	return func() *gocqlx.Queryx { return gocqlx.Query(s.Query(stmt), names) }
}

func NewCreateQuery(s *gocql.Session, lifetime time.Duration) QueryFunc {
	stmt, names := scyllaqb.
		Insert(table).
		Columns("id", "name", "size", "checksum").
		TTL(lifetime).
		ToCql()
	return func() *gocqlx.Queryx { return gocqlx.Query(s.Query(stmt), names) }
}
