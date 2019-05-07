package kipp

import "time"

// Option applies the specified option to the Handler.
type Option func(*Handler)

// Store will provide a store for the Handler. to store entities in.
func Store(e EntityCreator) Option { return func(h *Handler) { h.store = e } }

// Lifetime is how long a file will be available before being deleted.
func Lifetime(lifetime time.Duration) Option { return func(h *Handler) { h.lifetime = lifetime } }

// Max is the maximum file size.
func Max(max int64) Option { return func(h *Handler) { h.max = max } }

// Path is where kipp will store files.
func Path(p string) Option { return func(h *Handler) { h.path = p } }

// Web is where kipp will server web assets from.
func Web(p string) Option { return func(s *Handler) { s.web = p } }
