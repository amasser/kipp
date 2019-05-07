package kipp

import (
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"mime"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gabriel-vasile/mimetype"
	"github.com/minio/blake2b-simd"
)

// Handler acts as the HTTP server, configuration and provides essential core
// functions such as Cleanup.
type Handler struct {
	store    EntityCreator
	lifetime time.Duration
	max      int64
	path     string
	web      string
}

// New will create a new Handler with some sensible defaults applied. It will then apply opts.
func New(opts ...Option) *Handler {
	h := new(Handler)
	for _, opt := range opts {
		opt(h)
	}
	return h
}

// // Cleanup will delete expired files and remove files associated with it as
// // long as it is not used by any other files.
// func (s Handler) Cleanup() (err error) {
// 	var b [8]byte
// 	binary.BigEndian.PutUint64(b[:], uint64(time.Now().Unix()))
//
// 	tx, err := s.DB.Begin(true)
// 	if err != nil {
// 		return err
// 	}
// 	defer func() {
// 		if e := tx.Commit(); err == nil {
// 			err = e
// 		}
// 	}()
//
// 	c := tx.Bucket([]byte("ttl")).Cursor()
// 	for k, v := c.First(); k != nil && bytes.Compare(k, b[:]) <= 0; k, v = c.Next() {
// 		if err := c.Delete(); err != nil {
// 			return err
// 		}
// 		sum := base64.RawURLEncoding.EncodeToString(v)
// 		if err := os.Remove(filepath.Join(s.files, sum)); err != nil {
// 			return err
// 		}
// 	}
// 	return nil
// }

// ServeHTTP will serve HTTP requests. It first tries to determine if the
// request is for uploading, it then tries to serve static files and then will
// try to serve public files.

// ServeHTTP will route requests depending on the method and path.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.Method == http.MethodPost && r.URL.Path == "/":
		h.Upload(w, r)
	case r.Method == http.MethodGet || r.Method == http.MethodHead:
		h.Files(w, r)
	default:
		h.Allow(w, r)
	}
}

// Allow will set access control headers and reject requests using invalid methods.
func (h *Handler) Allow(w http.ResponseWriter, r *http.Request) {
	allow := "GET, HEAD, OPTIONS"
	if r.URL.Path == "/" {
		allow = "GET, HEAD, OPTIONS, POST"
	}
	switch r.Method {
	case http.MethodOptions:
		w.Header().Set("Access-Control-Allow-Methods", allow)
	default:
		w.Header().Set("Allow", allow)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
	}
}

func (h *Handler) Files(w http.ResponseWriter, r *http.Request) {
	http.FileServer(httpFileSystemFunc(func(name string) (http.File, error) {
		f, err := http.Dir(h.web).Open(name)
		if err == nil {
			d, err := f.Stat()
			if err != nil {
				return nil, err
			}
			if !d.IsDir() {
				w.Header().Set("Cache-Control", "max-age=31536000")
				// nginx style weak Etag
				w.Header().Set("Etag", fmt.Sprintf(
					`W/"%x-%x"`,
					d.ModTime().Unix(), d.Size(),
				))
			}
			return f, nil
		}
		dir, name := path.Split(name)
		if dir != "/" {
			return nil, os.ErrNotExist
		}
		// trim anything after the first "."
		if i := strings.Index(name, "."); i > -1 {
			name = name[:i]
		}
		e, err := h.store.Entity(name)
		if err != nil {
			return nil, err
		}
		// 1 year
		cache := "max-age=31536000"
		if e.Lifetime != nil {
			// duration in seconds until expiration
			d := int(time.Until(*e.Lifetime).Seconds())
			// if the httpFile has since become expired since we queried, invalidate it.
			if d <= 0 {
				return nil, os.ErrNotExist
			}
			cache = "public, must-revalidate, max-age=" + strconv.Itoa(d)
		}
		f, err = os.Open(filepath.Join(h.path, e.Checksum))
		if err != nil {
			return nil, err
		}
		// Detect content type before serving content to filter html files
		ctype := mime.TypeByExtension(filepath.Ext(e.Name))
		if ctype == "" {
			ctype, _, err = mimetype.DetectReader(f)
			if err != nil {
				return nil, err
			}
			if _, err := f.Seek(0, io.SeekStart); err != nil {
				return nil, errors.New("seeker can't seek")
			}
		}
		// catches text/html and text/html; charset=utf-8
		const prefix = "text/html"
		if strings.HasPrefix(ctype, prefix) {
			ctype = "text/plain" + ctype[len(prefix):]
		}
		w.Header().Set("Cache-Control", cache)
		w.Header().Set("Content-Disposition", fmt.Sprintf(
			"filename=%q; filename*=UTF-8''%[1]s",
			url.PathEscape(e.Name),
		))
		w.Header().Set("Content-Type", ctype)
		w.Header().Set("Etag", strconv.Quote(e.Checksum))
		if e.Lifetime != nil {
			w.Header().Set("Expires", e.Lifetime.Format(http.TimeFormat))
		}
		w.Header().Set("X-Content-Type-Options", "nosniff")
		return httpFile{f, e.Timestamp}, nil
	})).ServeHTTP(w, r)
}

// Upload will hash and write the multipart body to disk before linking the temporary file to a new location based on
// the calculated hash. It will then insert the entity into the store and return SeeOther with a body containing the id
// of the uploaded file.
func (h *Handler) Upload(w http.ResponseWriter, r *http.Request) {
	if r.ContentLength > h.max {
		http.Error(w, http.StatusText(http.StatusRequestEntityTooLarge), http.StatusRequestEntityTooLarge)
		return
	}

	// We limit the body here to keep it consistent with the sent ContentLength header, even if it means files
	// smaller than the max will be rejected.
	r.Body = http.MaxBytesReader(w, r.Body, h.max)

	// Find the multipart body to read from.
	mr, err := r.MultipartReader()
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	var p *multipart.Part
	for {
		p, err = mr.NextPart()
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if p.FormName() == "file" {
			break
		}
	}

	name := p.FileName()
	if len(name) > 255 {
		http.Error(w, "invalid name", http.StatusBadRequest)
		return
	}

	tf, err := ioutil.TempFile(h.path, "kipptmp")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer os.Remove(tf.Name())
	defer tf.Close()

	// Hash the httpFile and write to disk.
	hs := blake2b.New512()
	n, err := io.Copy(io.MultiWriter(tf, hs), p)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	sum := base64.RawURLEncoding.EncodeToString(hs.Sum(nil))

	id, err := h.store.Create(name, uint64(n), sum)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// link instead of rename since it will not overwrite the original httpFile if it exists, also prevents race
	// conditions using stat.
	if err := os.Link(tf.Name(), filepath.Join(h.path, sum)); err != nil && !os.IsExist(err) {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// '/' + id + ext + '\n'
	ext := filepath.Ext(name)
	var b strings.Builder
	b.Grow(len(id) + len(ext) + 2)
	b.WriteRune('/')
	b.WriteString(id)
	b.WriteString(ext)

	http.Redirect(w, r, b.String(), http.StatusSeeOther)

	b.WriteRune('\n')
	io.Copy(w, strings.NewReader(b.String()))
}
