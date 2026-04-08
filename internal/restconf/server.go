package restconf

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"log"
	"math/big"
	"net"
	"net/http"
	"time"

	"github.com/vimukthi/cisco-wlc-sim/internal/accesslog"
	"github.com/vimukthi/cisco-wlc-sim/internal/config"
	"github.com/vimukthi/cisco-wlc-sim/internal/device"
)

// Serve starts an HTTPS RESTCONF server for the given device.
func Serve(dev *device.Device, auth config.Auth, logs *accesslog.Store) error {
	mux := http.NewServeMux()

	// RESTCONF root
	mux.HandleFunc("/.well-known/host-meta", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xrd+xml")
		fmt.Fprint(w, `<XRD xmlns="http://docs.oasis-open.org/ns/xri/xrd-1.0"><Link rel="restconf" href="/restconf"/></XRD>`)
	})

	// Client oper data - full
	mux.HandleFunc("/restconf/data/Cisco-IOS-XE-wireless-client-oper:client-oper-data", func(w http.ResponseWriter, r *http.Request) {
		handleClientOperData(w, r, dev)
	})

	// Sub-endpoints
	mux.HandleFunc("/restconf/data/Cisco-IOS-XE-wireless-client-oper:client-oper-data/common-oper-data", func(w http.ResponseWriter, r *http.Request) {
		handleCommonOperData(w, r, dev)
	})
	mux.HandleFunc("/restconf/data/Cisco-IOS-XE-wireless-client-oper:client-oper-data/dot11-oper-data", func(w http.ResponseWriter, r *http.Request) {
		handleDot11OperData(w, r, dev)
	})
	mux.HandleFunc("/restconf/data/Cisco-IOS-XE-wireless-client-oper:client-oper-data/traffic-stats", func(w http.ResponseWriter, r *http.Request) {
		handleTrafficStats(w, r, dev)
	})
	mux.HandleFunc("/restconf/data/Cisco-IOS-XE-wireless-client-oper:client-oper-data/sisf-db-mac", func(w http.ResponseWriter, r *http.Request) {
		handleSisfDbMac(w, r, dev)
	})
	mux.HandleFunc("/restconf/data/Cisco-IOS-XE-wireless-client-oper:client-oper-data/dc-info", func(w http.ResponseWriter, r *http.Request) {
		handleDcInfo(w, r, dev)
	})
	mux.HandleFunc("/restconf/data/Cisco-IOS-XE-wireless-client-oper:client-oper-data/policy-data", func(w http.ResponseWriter, r *http.Request) {
		handlePolicyData(w, r, dev)
	})

	// Wrap with basic auth and logging
	handler := logRequests(basicAuth(mux, auth), dev, logs)

	addr := fmt.Sprintf("%s:%d", dev.IP, dev.HTTPSPort)
	tlsCert, err := generateSelfSignedCert(dev.IP, dev.Hostname)
	if err != nil {
		return fmt.Errorf("generate TLS cert: %w", err)
	}

	server := &http.Server{
		Addr:    addr,
		Handler: handler,
		TLSConfig: &tls.Config{
			Certificates: []tls.Certificate{tlsCert},
		},
	}

	log.Printf("[%s] RESTCONF listening on https://%s", dev.Hostname, addr)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen %s: %w", addr, err)
	}
	tlsLn := tls.NewListener(ln, server.TLSConfig)
	return server.Serve(tlsLn)
}

func basicAuth(next http.Handler, auth config.Auth) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := r.BasicAuth()
		if !ok || user != auth.Username || pass != auth.Password {
			w.Header().Set("WWW-Authenticate", `Basic realm="RESTCONF"`)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

type statusWriter struct {
	http.ResponseWriter
	code int
}

func (sw *statusWriter) WriteHeader(code int) {
	sw.code = code
	sw.ResponseWriter.WriteHeader(code)
}

func logRequests(next http.Handler, dev *device.Device, logs *accesslog.Store) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sw := &statusWriter{ResponseWriter: w, code: 200}
		next.ServeHTTP(sw, r)
		user, _, _ := r.BasicAuth()
		logs.Add(accesslog.Entry{
			DeviceHost: dev.Hostname,
			DeviceIP:   dev.IP,
			Type:       "restconf",
			Source:      r.RemoteAddr,
			Method:     r.Method,
			Path:       r.URL.Path,
			Status:     sw.code,
			User:       user,
		})
	})
}

func generateSelfSignedCert(ip, hostname string) (tls.Certificate, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, err
	}

	serial, _ := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	tmpl := x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			CommonName:   hostname,
			Organization: []string{"Cisco Systems"},
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(10 * 365 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		DNSNames:              []string{hostname},
	}
	if parsedIP := net.ParseIP(ip); parsedIP != nil {
		tmpl.IPAddresses = []net.IP{parsedIP}
	}

	certDER, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &key.PublicKey, key)
	if err != nil {
		return tls.Certificate{}, err
	}

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return tls.Certificate{}, err
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})

	return tls.X509KeyPair(certPEM, keyPEM)
}
