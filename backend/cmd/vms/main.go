package main

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"flag"
	"log"
	"math/big"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"time"

	"vms.local/backend/internal/app"
	"vms.local/backend/internal/servicehost"
)

func main() {
	listen := flag.String("listen", "127.0.0.1:18443", "HTTPS address")
	next := flag.String("frontend", "http://127.0.0.1:13000", "private Next.js server")
	data := flag.String("data", "", "installation data directory")
	cert := flag.String("cert", "", "TLS certificate PEM")
	key := flag.String("key", "", "TLS private key PEM")
	tlsHost := flag.String("tls-host", "localhost", "DNS name or IP included in a new local certificate")
	worker := flag.String("worker", "http://127.0.0.1:19091", "private media worker")
	whep := flag.String("whep", "http://127.0.0.1:18889", "private WHEP endpoint")
	flag.Parse()
	if *data == "" || os.Getenv("VMS_DATABASE_URL") == "" {
		log.Fatal("data directory and VMS_DATABASE_URL required")
	}
	if err := os.MkdirAll(*data, 0700); err != nil {
		log.Fatal(err)
	}
	if *cert == "" && *key == "" {
		*cert = filepath.Join(*data, "localhost.crt")
		*key = filepath.Join(*data, "localhost.key")
		if _, err := os.Stat(*cert); os.IsNotExist(err) {
			if err := createLocalCertificate(*cert, *key, *tlsHost); err != nil {
				log.Fatal(err)
			}
		}
	}
	target, err := url.Parse(*next)
	if err != nil || target.Scheme != "http" || net.ParseIP(target.Hostname()) == nil || !net.ParseIP(target.Hostname()).IsLoopback() {
		log.Fatal("frontend must use a loopback HTTP address")
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	servicehost.WatchParent(cancel)
	api, err := app.New(ctx, app.Options{DatabaseURL: os.Getenv("VMS_DATABASE_URL"), SecureCookies: true, DataDir: *data, WorkerURL: *worker, WHEPURL: *whep, WorkerSecret: os.Getenv("VMS_WORKER_SECRET")})
	if err != nil {
		log.Fatal("could not initialize database; verify local database configuration")
	}
	defer api.Close()
	apiHandler := api.Handler()
	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		http.Error(w, "Interface indisponível. Verifique o serviço web.", 503)
	}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			apiHandler.ServeHTTP(w, r)
			return
		}
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		proxy.ServeHTTP(w, r)
	})
	server := &http.Server{Addr: *listen, Handler: handler, ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 60 * time.Second, TLSConfig: &tls.Config{MinVersion: tls.VersionTLS12}}
	drained := make(chan struct{})
	go func() {
		defer close(drained)
		select {
		case <-ctx.Done():
		case <-api.Done():
			cancel()
		}
		shutdown, done := context.WithTimeout(context.Background(), 5*time.Second)
		defer done()
		if server.Shutdown(shutdown) != nil {
			server.Close()
		}
	}()
	log.Printf("VMS HTTPS listening on %s", *listen)
	if err := server.ListenAndServeTLS(*cert, *key); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
	cancel()
	<-drained
}
func createLocalCertificate(certPath, keyPath, host string) error {
	private, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return err
	}
	template := x509.Certificate{SerialNumber: serial, Subject: pkix.Name{CommonName: "VMS local"}, NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().AddDate(1, 0, 0), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, DNSNames: []string{"localhost"}, IPAddresses: []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("::1")}}
	if ip := net.ParseIP(host); ip != nil {
		template.IPAddresses = append(template.IPAddresses, ip)
	} else if host != "" && host != "localhost" {
		template.DNSNames = append(template.DNSNames, host)
	}
	raw, err := x509.CreateCertificate(rand.Reader, &template, &template, &private.PublicKey, private)
	if err != nil {
		return err
	}
	privateRaw, err := x509.MarshalPKCS8PrivateKey(private)
	if err != nil {
		return err
	}
	if err := os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privateRaw}), 0600); err != nil {
		return err
	}
	return os.WriteFile(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: raw}), 0600)
}
