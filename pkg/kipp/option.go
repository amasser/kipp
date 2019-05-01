package kipp

import (
	"path/filepath"
	"time"
)

type Option func(*Server)

func Path(p string) Option {
	return func(s *Server) {
		s.filePath = filepath.Join(p, "files")
		s.tmpPath = filepath.Join(p, "tmp")
	}
}

func Max(max int64) Option {
	return func(s *Server) {
		s.max = max
	}
}

func Lifetime(lifetime time.Duration) Option {
	return func(s *Server) {
		s.lifetime = lifetime
	}
}
