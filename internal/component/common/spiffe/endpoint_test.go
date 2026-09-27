package spiffe_test

import (
	"io"
	"net/http"
	"testing"

	promconfig "github.com/prometheus/common/config"
	"github.com/spiffe/go-spiffe/v2/bundle/x509bundle"
	"github.com/stretchr/testify/require"

	types "github.com/grafana/alloy/internal/component/common/config"
	"github.com/grafana/alloy/internal/component/common/spiffe"
	"github.com/grafana/alloy/internal/component/common/spiffe/spiffetest"
)

const (
	clientID = "spiffe://example.org/alloy"
	serverID = "spiffe://example.org/mimir"
)

func newClient(t *testing.T, cfg *spiffe.EndpointConfig) *http.Client {
	t.Helper()
	c, err := promconfig.NewClientFromConfig(promconfig.HTTPClientConfig{}, "test", cfg.HTTPClientOptions()...)
	require.NoError(t, err)
	return c
}

func get(c *http.Client, url string) (string, error) {
	resp, err := c.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	return string(b), err
}

func TestURIOnlyServerSVIDAccepted(t *testing.T) {
	ca := spiffetest.NewCA(t, "example.org")
	srv := spiffetest.NewMTLSServer(t, ca.Bundle(), ca.SVID(t, serverID), spiffetest.EchoClientID())
	src := spiffe.NewSource()
	src.OnX509ContextUpdate(ca.X509Context(ca.SVID(t, clientID)))

	body, err := get(newClient(t, &spiffe.EndpointConfig{Source: src, ServerIDs: []string{serverID}}), srv.URL)
	require.NoError(t, err)
	require.Equal(t, clientID, body)
}

// stockClient verifies the server the way a plain tls_config does: by DNS name.
func stockClient(t *testing.T, ca *spiffetest.CA) *http.Client {
	t.Helper()
	certPEM, keyPEM, err := ca.SVID(t, clientID).Marshal()
	require.NoError(t, err)
	c, err := promconfig.NewClientFromConfig(promconfig.HTTPClientConfig{
		TLSConfig: promconfig.TLSConfig{
			CA:         string(ca.CertPEM()),
			Cert:       string(certPEM),
			Key:        promconfig.Secret(keyPEM),
			ServerName: "mimir.example",
		},
	}, "test")
	require.NoError(t, err)
	return c
}

func TestStockTLSConfigRejectsURIOnlyServerSVID(t *testing.T) {
	ca := spiffetest.NewCA(t, "example.org")
	srv := spiffetest.NewMTLSServer(t, ca.Bundle(), ca.SVID(t, serverID), spiffetest.EchoClientID())

	_, err := get(stockClient(t, ca), srv.URL)
	require.ErrorContains(t, err, "x509: certificate is not valid for any names, but wanted to match mimir.example")
}

func TestStockTLSConfigAcceptsServerSVIDWithDNSSAN(t *testing.T) {
	ca := spiffetest.NewCA(t, "example.org")
	srv := spiffetest.NewMTLSServer(t, ca.Bundle(), ca.SVID(t, serverID, "mimir.example"), spiffetest.EchoClientID())

	body, err := get(stockClient(t, ca), srv.URL)
	require.NoError(t, err)
	require.Equal(t, clientID, body)
}

func TestServerIDMismatchRejected(t *testing.T) {
	ca := spiffetest.NewCA(t, "example.org")
	srv := spiffetest.NewMTLSServer(t, ca.Bundle(), ca.SVID(t, "spiffe://example.org/other"), spiffetest.EchoClientID())
	src := spiffe.NewSource()
	src.OnX509ContextUpdate(ca.X509Context(ca.SVID(t, clientID)))

	_, err := get(newClient(t, &spiffe.EndpointConfig{Source: src, ServerIDs: []string{serverID}}), srv.URL)
	require.ErrorContains(t, err, `unexpected ID "spiffe://example.org/other"`)
}

func TestUntrustedTrustDomainRejected(t *testing.T) {
	ca := spiffetest.NewCA(t, "example.org")
	other := spiffetest.NewCA(t, "other.org")
	trusted := x509bundle.NewSet(ca.Bundle(), other.Bundle())
	srv := spiffetest.NewMTLSServer(t, trusted, other.SVID(t, "spiffe://other.org/mimir"), spiffetest.EchoClientID())
	src := spiffe.NewSource()
	src.OnX509ContextUpdate(ca.X509Context(ca.SVID(t, clientID)))

	_, err := get(newClient(t, &spiffe.EndpointConfig{Source: src, ServerIDs: []string{"spiffe://other.org/mimir"}}), srv.URL)
	require.ErrorContains(t, err, `no X.509 bundle for trust domain "other.org"`)
}

func TestNoSVIDYetFailsHandshake(t *testing.T) {
	ca := spiffetest.NewCA(t, "example.org")
	srv := spiffetest.NewMTLSServer(t, ca.Bundle(), ca.SVID(t, serverID), spiffetest.EchoClientID())

	_, err := get(newClient(t, &spiffe.EndpointConfig{Source: spiffe.NewSource(), ServerIDs: []string{serverID}}), srv.URL)
	require.Error(t, err)
}

func TestRotationAppliesToNewConnections(t *testing.T) {
	ca := spiffetest.NewCA(t, "example.org")
	srv := spiffetest.NewMTLSServer(t, ca.Bundle(), ca.SVID(t, serverID), spiffetest.EchoClientID())
	src := spiffe.NewSource()
	src.OnX509ContextUpdate(ca.X509Context(ca.SVID(t, clientID)))
	c := newClient(t, &spiffe.EndpointConfig{Source: src, ServerIDs: []string{serverID}})

	body, err := get(c, srv.URL)
	require.NoError(t, err)
	require.Equal(t, clientID, body)

	src.OnX509ContextUpdate(ca.X509Context(ca.SVID(t, "spiffe://example.org/alloy-rotated")))
	c.CloseIdleConnections()
	body, err = get(c, srv.URL)
	require.NoError(t, err)
	require.Equal(t, "spiffe://example.org/alloy-rotated", body)
}

func TestEndpointConfigValidate(t *testing.T) {
	src := spiffe.NewSource()
	require.NoError(t, (&spiffe.EndpointConfig{Source: src, ServerIDs: []string{serverID}}).Validate())
	require.ErrorContains(t, (&spiffe.EndpointConfig{ServerIDs: []string{serverID}}).Validate(), "source")
	require.ErrorContains(t, (&spiffe.EndpointConfig{Source: src}).Validate(), "server_ids")
	require.ErrorContains(t, (&spiffe.EndpointConfig{Source: src, ServerIDs: []string{"mimir"}}).Validate(), `invalid server_ids entry "mimir"`)
}

func TestValidateEndpoint(t *testing.T) {
	require.NoError(t, spiffe.ValidateEndpoint("https://mimir.example/api/v1/push", nil))
	require.NoError(t, spiffe.ValidateEndpoint("https://mimir.example/api/v1/push", &types.TLSConfig{ServerName: "mimir"}))
	require.ErrorContains(t, spiffe.ValidateEndpoint("http://mimir.example/api/v1/push", nil), "https")
	require.ErrorContains(t, spiffe.ValidateEndpoint("https://mimir.example", &types.TLSConfig{CAFile: "/ca.pem"}), "tls_config")
	require.ErrorContains(t, spiffe.ValidateEndpoint("https://mimir.example", &types.TLSConfig{InsecureSkipVerify: true}), "tls_config")
}

func TestNilEndpointConfigHasNoOptions(t *testing.T) {
	var cfg *spiffe.EndpointConfig
	require.Nil(t, cfg.HTTPClientOptions())
}
