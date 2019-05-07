package kipp_test

import (
	"errors"
	"io"
	"io/ioutil"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/uhthomas/kipp/pkg/kipp"
)

type infiniteReader struct{}

func (infiniteReader) Read(b []byte) (n int, err error) { return len(b), nil }

type fakeStore struct {
	EntityEntity *kipp.Entity
	FileErr      error

	CreateString string
	CreateErr    error
}

func (s fakeStore) Entity(id string) (*kipp.Entity, error) { return s.EntityEntity, s.FileErr }

func (s fakeStore) Create(name string, size uint64, checksum string) (string, error) {
	return s.CreateString, s.CreateErr
}

func TestServer_ServeHTTP(t *testing.T) {
	lifetime := time.Now().Add(time.Hour)
	content := strings.NewReader("some content")
	f := &kipp.Entity{
		ID:        "some-id",
		Checksum:  "some-checksum",
		Name:      "some-name",
		Size:      uint64(content.Len()),
		Lifetime:  &lifetime,
		Timestamp: time.Now(),
	}

	tmpdir, err := ioutil.TempDir("", "kipp")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmpdir)

	if err := os.Mkdir(filepath.Join(tmpdir, "files"), 0777); err != nil {
		t.Fatal(err)
	}

	ff, err := os.Create(filepath.Join(tmpdir, "files", f.Checksum))
	if err != nil {
		t.Fatal(err)
	}

	if _, err := io.Copy(ff, content); err != nil {
		t.Fatal(err)
	}

	t.Run("should serve a httpFile", func(t *testing.T) {
		s := kipp.New(
			kipp.Store(fakeStore{EntityEntity: f}),
			kipp.Path(tmpdir),
		)

		r := httptest.NewRequest(http.MethodGet, "/"+f.ID, nil)

		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)

		if e, c := http.StatusOK, w.Result().StatusCode; c != e {
			t.Errorf("Expected %d, got %d", e, c)
		}
	})
}

func TestServer_UploadHandler(t *testing.T) {
	multipartReader := func(name string, r io.Reader) (string, io.Reader) {
		pr, pw := io.Pipe()
		w := multipart.NewWriter(pw)
		go func() {
			defer pw.Close()
			fw, err := w.CreateFormFile("file", name)
			if err != nil {
				pr.CloseWithError(err)
				return
			}
			if _, err := io.Copy(fw, r); err != nil {
				pr.CloseWithError(err)
				return
			}
			pr.CloseWithError(w.Close())
		}()
		return w.FormDataContentType(), pr
	}

	t.Run("should upload httpFile and redirect", func(t *testing.T) {
		dir, err := ioutil.TempDir("", "kipp")
		if err != nil {
			t.Fatal(err)
		}
		defer os.Remove(dir)

		expid := "some-id"

		if err := os.MkdirAll(filepath.Join(dir, "files"), 0777); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Join(dir, "path"), 0777); err != nil {
			t.Fatal(err)
		}

		s := kipp.New(
			kipp.Store(fakeStore{CreateString: expid}),
			kipp.Max(1<<10),
			kipp.Path(dir),
		)

		ct, rr := multipartReader("test.txt", strings.NewReader("test"))
		r := httptest.NewRequest(http.MethodPost, "/", rr)
		r.Header.Set("Content-Type", ct)

		w := httptest.NewRecorder()
		s.Upload(w, r)

		if e, c := http.StatusSeeOther, w.Result().StatusCode; c != e {
			t.Errorf("Expected %d, got %d", e, c)
		}

		var b strings.Builder
		if _, err := io.Copy(&b, w.Result().Body); err != nil {
			t.Fatal(err)
		}
		expid = "/" + expid + ".txt\n"
		if b.String() != expid {
			t.Fatalf("expected %s, got %s", expid, b.String())
		}
	})

	t.Run("should reject files with a content-length header larger than the max size", func(t *testing.T) {
		// 1 MB
		s := kipp.New(kipp.Max(1 << 10))

		r := httptest.NewRequest(http.MethodPost, "/", nil)
		// 2 MB
		r.ContentLength = 2 << 10

		w := httptest.NewRecorder()
		s.Upload(w, r)

		if e, c := http.StatusRequestEntityTooLarge, w.Result().StatusCode; c != e {
			t.Errorf("Expected %d, got %d", e, c)
		}
	})

	t.Run("should reject files with a body larger than the max size", func(t *testing.T) {
		// 1 MB
		s := kipp.New(kipp.Max(1 << 10))

		w := httptest.NewRecorder()
		s.Upload(w, httptest.NewRequest(http.MethodPost, "/", new(infiniteReader)))

		if e, c := http.StatusBadRequest, w.Result().StatusCode; c != e {
			t.Errorf("Expected %d, got %d", e, c)
		}
	})

	t.Run("should not allow names longer than 255 characters", func(t *testing.T) {
		s := kipp.New(kipp.Max(1 << 10))

		ct, rr := multipartReader(strings.Repeat("a", 256), io.LimitReader(nil, 0))
		r := httptest.NewRequest(http.MethodPost, "/", rr)
		r.Header.Set("Content-Type", ct)

		w := httptest.NewRecorder()
		s.Upload(w, r)

		if e, c := http.StatusBadRequest, w.Result().StatusCode; c != e {
			t.Errorf("Expected %d, got %d", e, c)
		}
	})

	t.Run("should fail when store fails", func(t *testing.T) {
		experr := errors.New("expected")
		s := kipp.New(
			kipp.Store(fakeStore{CreateErr: experr}),
			kipp.Max(1<<10),
		)

		ct, rr := multipartReader("test.txt", io.LimitReader(nil, 0))
		r := httptest.NewRequest(http.MethodPost, "/", rr)
		r.Header.Set("Content-Type", ct)

		w := httptest.NewRecorder()
		s.Upload(w, r)

		if e, c := http.StatusInternalServerError, w.Result().StatusCode; c != e {
			t.Errorf("Expected %d, got %d", e, c)
		}
	})
}
