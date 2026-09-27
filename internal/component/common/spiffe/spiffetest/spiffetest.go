// Package spiffetest provides test CAs, SVIDs, and servers for SPIFFE mTLS tests.
package spiffetest

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/spiffe/go-spiffe/v2/bundle/x509bundle"
	"github.com/spiffe/go-spiffe/v2/spiffeid"
	"github.com/spiffe/go-spiffe/v2/spiffetls/tlsconfig"
	"github.com/spiffe/go-spiffe/v2/svid/x509svid"
	"github.com/spiffe/go-spiffe/v2/workloadapi"
	"github.com/stretchr/testify/require"
)

// CA is a single-certificate CA for one trust domain.
type CA struct {
	TrustDomain spiffeid.TrustDomain
	cert        *x509.Certificate
	key         crypto.Signer
}

func NewCA(t testing.TB, trustDomain string) *CA {
	t.Helper()
	td := spiffeid.RequireTrustDomainFromString(trustDomain)
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: trustDomain + " test CA"},
		URIs:                  []*url.URL{td.ID().URL()},
		NotBefore:             time.Now().Add(-time.Minute),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, key.Public(), key)
	require.NoError(t, err)
	cert, err := x509.ParseCertificate(der)
	require.NoError(t, err)
	return &CA{TrustDomain: td, cert: cert, key: key}
}

// SVID issues a leaf X509-SVID; with no dnsNames the certificate carries only the URI SAN.
func (ca *CA) SVID(t testing.TB, id string, dnsNames ...string) *x509svid.SVID {
	t.Helper()
	spiffeID := spiffeid.RequireFromString(id)
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	serial, err := rand.Int(rand.Reader, big.NewInt(1<<62))
	require.NoError(t, err)
	tmpl := &x509.Certificate{
		SerialNumber:          serial,
		URIs:                  []*url.URL{spiffeID.URL()},
		DNSNames:              dnsNames,
		NotBefore:             time.Now().Add(-time.Minute),
		NotAfter:              time.Now().Add(time.Hour),
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, ca.cert, key.Public(), ca.key)
	require.NoError(t, err)
	cert, err := x509.ParseCertificate(der)
	require.NoError(t, err)
	return &x509svid.SVID{ID: spiffeID, Certificates: []*x509.Certificate{cert}, PrivateKey: key}
}

func (ca *CA) Bundle() *x509bundle.Bundle {
	return x509bundle.FromX509Authorities(ca.TrustDomain, []*x509.Certificate{ca.cert})
}

func (ca *CA) CertPEM() []byte {
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: ca.cert.Raw})
}

func (ca *CA) X509Context(svid *x509svid.SVID) *workloadapi.X509Context {
	return &workloadapi.X509Context{
		SVIDs:   []*x509svid.SVID{svid},
		Bundles: x509bundle.NewSet(ca.Bundle()),
	}
}

// NewMTLSServer starts an HTTPS server presenting svid and requiring a client SVID verifiable by trusted.
func NewMTLSServer(t testing.TB, trusted x509bundle.Source, svid *x509svid.SVID, h http.Handler, opts ...func(*tls.Config)) *httptest.Server {
	t.Helper()
	cfg := tlsconfig.MTLSServerConfig(svid, trusted, tlsconfig.AuthorizeAny())
	// StartTLS adds httptest's own cert when Certificates is empty, and IP-addressed clients send no SNI to trigger GetCertificate.
	cfg.Certificates = []tls.Certificate{{
		Certificate: [][]byte{svid.Certificates[0].Raw},
		PrivateKey:  svid.PrivateKey,
		Leaf:        svid.Certificates[0],
	}}
	for _, opt := range opts {
		opt(cfg)
	}
	srv := httptest.NewUnstartedServer(h)
	srv.TLS = cfg
	srv.StartTLS()
	t.Cleanup(srv.Close)
	return srv
}

func EchoClientID() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(r.TLS.PeerCertificates[0].URIs[0].String()))
	})
}
