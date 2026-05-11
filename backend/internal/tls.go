package internal

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"time"
)

// EnsureSelfSignedCert creates a self-signed cert+key pair under certDir if
// they don't already exist. The cert covers the given host (IP or DNS) plus
// localhost / 127.0.0.1 / 0.0.0.0 so it works regardless of how clients reach
// us. Used for testing whether the Hik device's OTAP module will negotiate TLS
// against a non-CA cert.
func EnsureSelfSignedCert(certDir, host string) (certFile, keyFile string, err error) {
	if err := os.MkdirAll(certDir, 0o755); err != nil {
		return "", "", err
	}
	certFile = filepath.Join(certDir, "server.crt")
	keyFile = filepath.Join(certDir, "server.key")
	if statBoth(certFile, keyFile) {
		return certFile, keyFile, nil
	}

	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return "", "", fmt.Errorf("genkey: %w", err)
	}
	serial, err := rand.Int(rand.Reader, big.NewInt(1).Lsh(big.NewInt(1), 62))
	if err != nil {
		return "", "", err
	}

	cn := host
	if cn == "" {
		cn = "face_auth.local"
	}
	tmpl := x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			CommonName:   cn,
			Organization: []string{"face_auth"},
		},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().AddDate(10, 0, 0),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
	}

	if ip := net.ParseIP(host); ip != nil {
		tmpl.IPAddresses = append(tmpl.IPAddresses, ip)
	} else if host != "" {
		tmpl.DNSNames = append(tmpl.DNSNames, host)
	}
	// Always include loopback + any-IP fallbacks
	tmpl.IPAddresses = append(tmpl.IPAddresses,
		net.ParseIP("127.0.0.1"),
		net.ParseIP("0.0.0.0"),
	)
	tmpl.DNSNames = append(tmpl.DNSNames, "localhost", "face_auth.local")

	der, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &priv.PublicKey, priv)
	if err != nil {
		return "", "", fmt.Errorf("create cert: %w", err)
	}

	cf, err := os.Create(certFile)
	if err != nil {
		return "", "", err
	}
	defer cf.Close()
	if err := pem.Encode(cf, &pem.Block{Type: "CERTIFICATE", Bytes: der}); err != nil {
		return "", "", err
	}

	keyBytes, err := x509.MarshalECPrivateKey(priv)
	if err != nil {
		return "", "", err
	}
	kf, err := os.OpenFile(keyFile, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return "", "", err
	}
	defer kf.Close()
	if err := pem.Encode(kf, &pem.Block{Type: "EC PRIVATE KEY", Bytes: keyBytes}); err != nil {
		return "", "", err
	}
	return certFile, keyFile, nil
}

func statBoth(paths ...string) bool {
	for _, p := range paths {
		if _, err := os.Stat(p); err != nil {
			return false
		}
	}
	return true
}
