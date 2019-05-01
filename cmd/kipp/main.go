package main

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"log"
	"math/big"
	"mime"
	"net/http"
	"os"
	"time"

	"github.com/uhthomas/kipp/internal/scylla"
	"github.com/uhthomas/kipp/pkg/kipp"
	"gopkg.in/alecthomas/kingpin.v2"
)

type worker time.Duration

func (w worker) Do(ctx context.Context, f func() error) {
	for {
		if err := f(); err != nil {
			log.Fatal(err)
		}
		t := time.After(time.Duration(w))
		select {
		case <-ctx.Done():
			return
		case <-t:
		}
	}
}

func loadMimeTypes(path string) error {
	f, err := os.Open(path)
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()
	m := make(map[string][]string)
	if err := json.NewDecoder(f).Decode(&m); err != nil {
		return err
	}
	for k, v := range m {
		for _, vv := range v {
			mime.AddExtensionType(vv, k)
		}
	}
	return nil
}

func certificateGetter(certFile, keyFile string) func(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
	var cached *tls.Certificate
	return func(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
		if cached != nil {
			return cached, nil
		}
		if certFile != "" && keyFile != "" {
			cert, err := tls.LoadX509KeyPair(certFile, keyFile)
			if err != nil {
				return nil, err
			}
			cached = &cert
			return cached, nil
		}
		// Generate a self-signed certificate
		sn, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
		if err != nil {
			return nil, err
		}
		now := time.Now()
		t := &x509.Certificate{
			SerialNumber:          sn,
			NotBefore:             now,
			NotAfter:              now,
			KeyUsage:              x509.KeyUsageCertSign,
			ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
			BasicConstraintsValid: true,
			IsCA:                  true,
		}
		k, err := rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			return nil, err
		}
		c, err := x509.CreateCertificate(rand.Reader, t, t, &k.PublicKey, k)
		if err != nil {
			return nil, err
		}
		cert, err := tls.X509KeyPair(
			pem.EncodeToMemory(&pem.Block{
				Type:  "CERTIFICATE",
				Bytes: c,
			}),
			pem.EncodeToMemory(&pem.Block{
				Type:  "RSA PRIVATE KEY",
				Bytes: x509.MarshalPKCS1PrivateKey(k),
			}),
		)
		cached = &cert
		return cached, err
	}
}

func main() {
	servecmd := kingpin.Command("serve", "Start a kipp server.").Default()

	addr := servecmd.
		Flag("addr", "Server listen address.").
		Default("0.0.0.0:443").
		String()
	cert := servecmd.
		Flag("cert", "TLS certificate path.").
		String()
	key := servecmd.
		Flag("key", "TLS key path.").
		String()
	// cleanupInterval := servecmd.
	// 	Flag("cleanup-interval", "Cleanup interval for deleting expired files.").
	// 	Default("5m").
	// 	Duration()
	mime := servecmd.
		Flag("mime", "A json formatted collection of extensions and mime types.").
		PlaceHolder("PATH").
		String()
	// servecmd.
	// 	Flag("store", "Database file path.").
	// 	Default("kipp.db").
	// 	StringVar(&store)
	lifetime := servecmd.
		Flag("expiration", "File expiration time.").
		Default("24h").
		Duration()
	max := servecmd.
		Flag("max", "The maximum file size  for uploads.").
		Default("150MB").
		Bytes()
	p := servecmd.
		Flag("path", "File path.").
		Default("files").
		String()
	publicPath := servecmd.
		Flag("public", "Public path for web resources.").
		Default("public").
		String()

	var u UploadCommand
	{
		uploadcmd := kingpin.Command("upload", "Upload a file.")
		uploadcmd.
			Arg("file", "File to be uploaded").
			Required().
			FileVar(&u.File)
		uploadcmd.
			Flag("insecure", "Don't verify SSL certificates.").
			BoolVar(&u.Insecure)
		uploadcmd.
			Flag("private", "Encrypt the uploaded file").
			BoolVar(&u.Private)
		uploadcmd.
			Flag("url", "Source URL").
			Envar("kipp-upload-url").
			Default("https://kipp.6f.io").
			URLVar(&u.URL)
	}

	t := kingpin.Parse()

	// kipp upload
	if t == "upload" {
		u.Do()
		return
	}

	// Load mime types
	if m := *mime; m != "" {
		if err := loadMimeTypes(m); err != nil {
			log.Fatal(err)
		}
	}

	// Make paths for files and temp files
	if err := os.MkdirAll(*p, 0755); err != nil && !os.IsExist(err) {
		log.Fatal(err)
	}

	// Connect to database
	store, err := scylla.New("localhost:9042")
	if err != nil {
		log.Fatal(err)
	}
	s.Store = store

	// Start cleanup worker
	// if s.lifetime > 0 {
	// 	w := worker(*cleanupInterval)
	// 	go w.Do(context.Background(), s.Cleanup)
	// }

	var opts []kipp.Option
	if lifetime != nil {
		opts = append(opts, kipp.Lifetime(*lifetime))
	}
	if p != nil {
		opts = append(opts, kipp.Path(*p))
	}
	if max != nil {
		opts = append(opts, kipp.Max(int64(*max)))
	}
	s := kipp.New(opts...)
	s.PublicPath = *publicPath
	// Start HTTP server
	hs := &http.Server{
		Addr:    *addr,
		Handler: s,
		TLSConfig: &tls.Config{
			GetCertificate: certificateGetter(*cert, *key),
			CipherSuites: []uint16{
				tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
				tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
				tls.TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305,
				tls.TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305,
				tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
				tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
			},
			PreferServerCipherSuites: true,
			MinVersion:               tls.VersionTLS12,
			CurvePreferences:         []tls.CurveID{tls.CurveP256, tls.X25519},
		},
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  120 * time.Second,
	}
	// Output a message so users know when the server has been started.
	log.Printf("Listening on %s", *addr)
	log.Fatal(hs.ListenAndServeTLS("", ""))
}
