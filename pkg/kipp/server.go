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

	"github.com/minio/blake2b-simd"
)

// Server acts as the HTTP server, configuration and provides essential core
// functions such as Cleanup.
type Server struct {
	PublicPath string
	Store      Store
	lifetime   time.Duration
	max        int64
	filePath   string
	tmpPath    string
}

func New(opts ...Option) *Server {
	s := new(Server)
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// // Cleanup will delete expired files and remove files associated with it as
// // long as it is not used by any other files.
// func (s Server) Cleanup() (err error) {
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
// 		if err := os.Remove(filepath.Join(s.filePath, sum)); err != nil {
// 			return err
// 		}
// 	}
// 	return nil
// }

// ServeHTTP will serve HTTP requests. It first tries to determine if the
// request is for uploading, it then tries to serve static files and then will
// try to serve public files.
func (s Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/" && r.Method == http.MethodPost {
		s.UploadHandler(w, r)
		return
	}
	switch r.Method {
	case http.MethodGet, http.MethodHead:
	default:
		allow := "GET, HEAD, OPTIONS"
		if r.URL.Path == "/" {
			allow = "GET, HEAD, OPTIONS, POST"
		}
		if r.Method == http.MethodOptions {
			w.Header().Set("Access-Control-Allow-Methods", allow)
		} else {
			w.Header().Set("Allow", allow)
			http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		}
		return
	}
	http.FileServer(fileSystemFunc(func(name string) (http.File, error) {
		f, err := http.Dir(s.PublicPath).Open(name)
		if !os.IsNotExist(err) {
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
		out, err := s.Store.File(name)
		if err != nil {
			return nil, err
		}
		// 1 year
		cache := "max-age=31536000"
		if out.Expires != nil {
			// duration in seconds until expiration
			d := int(time.Until(*out.Expires).Seconds())
			if d <= 0 {
				// catch expired files. the cleanup worker should delete the
				// file on its own at some point
				return nil, os.ErrNotExist
			}
			cache = fmt.Sprintf("public, must-revalidate, max-age=%d", d)
		}
		f, err = os.Open(filepath.Join(s.filePath, out.Checksum))
		if err != nil {
			return nil, err
		}
		// Detect content type before serving content to filter html files
		ctype := mime.TypeByExtension(filepath.Ext(out.Name))
		if ctype == "" {
			var b [512]byte
			n, _ := io.ReadFull(f, b[:])
			ctype = http.DetectContentType(b[:n])
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
			url.PathEscape(out.Name),
		))
		w.Header().Set("Content-Type", ctype)
		w.Header().Set("Etag", strconv.Quote(out.Checksum))
		if out.Expires != nil {
			w.Header().Set("Expires", out.Expires.Format(http.TimeFormat))
		}
		w.Header().Set("X-Content-Type-Options", "nosniff")
		return file{f, out.Timestamp}, nil
	})).ServeHTTP(w, r)
}

// UploadHandler will read the request body and write it to the disk whilst also
// calculating a blake2b checksum. It will then insert the file information
// into the database and if the file doesn't already exist, it will be moved
// into the filePath. It will then return StatusSeeOther with the location
// of the file.
func (s Server) UploadHandler(w http.ResponseWriter, r *http.Request) {
	if r.ContentLength > s.max {
		http.Error(w, http.StatusText(http.StatusRequestEntityTooLarge), http.StatusRequestEntityTooLarge)
		return
	}
	// Find the multipart body to read from.
	mr, err := r.MultipartReader()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	var p *multipart.Part
	for {
		p, err = mr.NextPart()
		if err == io.EOF {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if p.FormName() == "file" {
			break
		}
	}
	defer p.Close()

	name := p.FileName()
	if len(name) > 255 {
		http.Error(w, "invalid name", http.StatusBadRequest)
		return
	}

	// Create temporary file to be used for storing uploads.
	tf, err := ioutil.TempFile(s.tmpPath, "kipptmp")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer os.Remove(tf.Name())
	defer tf.Close()

	// Hash the file and write to disk.
	h := blake2b.New512()
	n, err := io.Copy(io.MultiWriter(tf, h), http.MaxBytesReader(w, p, s.max))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	sum := base64.RawURLEncoding.EncodeToString(h.Sum(nil))

	id, err := s.Store.Create(name, uint64(n), sum)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// link instead of rename since it will not overwrite the original file if it exists, also prevents race
	// conditions using stat.
	if err := os.Link(tf.Name(), filepath.Join(s.filePath, sum)); err != nil && !os.IsExist(err) {
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
