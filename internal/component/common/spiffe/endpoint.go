package spiffe

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net/url"

	promconfig "github.com/prometheus/common/config"
	"github.com/spiffe/go-spiffe/v2/spiffeid"
	"github.com/spiffe/go-spiffe/v2/spiffetls/tlsconfig"

	types "github.com/grafana/alloy/internal/component/common/config"
)

// EndpointConfig is the spiffe block of an HTTP endpoint.
type EndpointConfig struct {
	Source    *Source  `alloy:"source,attr"`
	ServerIDs []string `alloy:"server_ids,attr"`
}

func (c *EndpointConfig) Validate() error {
	if c.Source == nil {
		return errors.New("spiffe: source must be set")
	}
	if len(c.ServerIDs) == 0 {
		return errors.New("spiffe: server_ids must list at least one SPIFFE ID")
	}
	_, err := c.serverIDs()
	return err
}

func (c *EndpointConfig) serverIDs() ([]spiffeid.ID, error) {
	ids := make([]spiffeid.ID, 0, len(c.ServerIDs))
	for _, raw := range c.ServerIDs {
		id, err := spiffeid.FromString(raw)
		if err != nil {
			return nil, fmt.Errorf("spiffe: invalid server_ids entry %q: %w", raw, err)
		}
		ids = append(ids, id)
	}
	return ids, nil
}

// HTTPClientOptions makes an HTTP client present the source's SVID and authorize the server by SPIFFE ID.
func (c *EndpointConfig) HTTPClientOptions() []promconfig.HTTPClientOption {
	if c == nil {
		return nil
	}
	return []promconfig.HTTPClientOption{promconfig.WithNewTLSConfigFunc(c.newTLSConfig)}
}

func (c *EndpointConfig) newTLSConfig(ctx context.Context, cfg *promconfig.TLSConfig, opts ...promconfig.TLSConfigOption) (*tls.Config, error) {
	ids, err := c.serverIDs()
	if err != nil {
		return nil, err
	}
	// Built from tls_config so server_name and min_version still apply; the hook replaces everything auth-related.
	base, err := promconfig.NewTLSConfigWithContext(ctx, cfg, opts...)
	if err != nil {
		return nil, err
	}
	tlsconfig.HookMTLSClientConfig(base, c.Source, c.Source, tlsconfig.AuthorizeOneOf(ids...))
	return base, nil
}

// ValidateEndpoint rejects endpoint settings that would bypass or conflict with SPIFFE mTLS.
func ValidateEndpoint(rawURL string, tlsCfg *types.TLSConfig) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return err
	}
	if u.Scheme != "https" {
		return fmt.Errorf("spiffe: endpoint url must use https, got %q", u.Scheme)
	}
	if tlsCfg != nil && (tlsCfg.CA != "" || tlsCfg.CAFile != "" || tlsCfg.Cert != "" || tlsCfg.CertFile != "" ||
		tlsCfg.Key != "" || tlsCfg.KeyFile != "" || tlsCfg.InsecureSkipVerify) {
		return errors.New("spiffe: cannot be combined with tls_config ca, cert, key or insecure_skip_verify settings")
	}
	return nil
}
