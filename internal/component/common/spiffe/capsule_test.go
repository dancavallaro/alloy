package spiffe_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/alloy/internal/component/common/spiffe"
	"github.com/grafana/alloy/internal/component/common/spiffe/spiffetest"
	"github.com/grafana/alloy/syntax/parser"
	"github.com/grafana/alloy/syntax/vm"
)

// The endpoint must see SVIDs that arrive after the config is evaluated.
func TestSourceSharedThroughConfigEvaluation(t *testing.T) {
	type exports struct {
		Source *spiffe.Source `alloy:"source,attr"`
	}
	src := spiffe.NewSource()

	f, err := parser.ParseFile("", []byte(`
		source     = agent.source
		server_ids = ["spiffe://example.org/mimir"]
	`))
	require.NoError(t, err)
	var cfg spiffe.EndpointConfig
	require.NoError(t, vm.New(f).Evaluate(vm.NewScope(map[string]any{"agent": exports{Source: src}}), &cfg))

	ca := spiffetest.NewCA(t, "example.org")
	src.OnX509ContextUpdate(ca.X509Context(ca.SVID(t, "spiffe://example.org/alloy")))

	svid, err := cfg.Source.GetX509SVID()
	require.NoError(t, err)
	require.Equal(t, "spiffe://example.org/alloy", svid.ID.String())
}
