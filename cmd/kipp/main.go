package main

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"log"
	"math/big"
	"net/http"
	"os"
	"time"

	"github.com/uhthomas/kipp/internal/scylla"
	"github.com/uhthomas/kipp/pkg/kipp"
	"gopkg.in/alecthomas/kingpin.v2"
)

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
	addr := kingpin.
		Flag("addr", "Handler listen address.").
		Default("0.0.0.0:443").
		String()
	cert := kingpin.
		Flag("cert", "TLS certificate path.").
		String()
	key := kingpin.
		Flag("key", "TLS key path.").
		String()
	// cleanupInterval := servecmd.
	// 	Flag("cleanup-interval", "Cleanup interval for deleting expired files.").
	// 	Default("5m").
	// 	Duration()
	// servecmd.
	// 	Flag("s", "Database file path.").
	// 	Default("kipp.db").
	// 	StringVar(&s)
	lifetime := kingpin.
		Flag("lifetime", "Entity lifetime.").
		Default("24h").
		Duration()
	max := kingpin.
		Flag("max", "The maximum file size  for uploads.").
		Default("150MB").
		Bytes()
	p := kingpin.
		Flag("path", "Entity path.").
		Default("data").
		String()
	web := kingpin.
		Flag("web", "Location of web resources.").
		Default("web").
		String()

	kingpin.Parse()

	// Make paths for files and temp files
	if err := os.MkdirAll(*p, 0755); err != nil && !os.IsExist(err) {
		log.Fatal(err)
	}

	// Connect to database
	s, err := scylla.New("localhost:9042")
	if err != nil {
		log.Fatal(err)
	}

	// Start cleanup worker
	// if h.lifetime > 0 {
	// 	w := worker(*cleanupInterval)
	// 	go w.Do(context.Background(), h.Cleanup)
	// }

	var opts []kipp.Option
	if s != nil {
		opts = append(opts, kipp.Store(s))
	}
	if lifetime != nil {
		opts = append(opts, kipp.Lifetime(*lifetime))
	}
	if max != nil {
		opts = append(opts, kipp.Max(int64(*max)))
	}
	if p != nil {
		opts = append(opts, kipp.Path(*p))
	}
	if web != nil {
		opts = append(opts, kipp.Web(*web))
	}

	// Output a message so users know when the server has been started.
	log.Printf("Listening on %s", *addr)
	log.Print("v1")
	log.Fatal((&http.Server{
		Addr:    *addr,
		Handler: kipp.New(opts...),
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
		// "ReadTimeout is the maximum duration for reading the entire
		// request, including the body."
		//
		// To allow files to be uploaded over a matter of minutes, this timeout must be a sensible value.
		ReadTimeout:       3 * time.Minute,
		ReadHeaderTimeout: 5 * time.Second,
		// The WriteTimeout must be at least equal or longer than ReadTimeout, therefore it is ReadTimeout + 5s.
		WriteTimeout: (3 * time.Minute) + (5 * time.Second),
		IdleTimeout:  2 * time.Minute,
	}).ListenAndServeTLS("", ""))
}
