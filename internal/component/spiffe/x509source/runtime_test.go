package x509source_test

import (
	"context"
	"fmt"
	"io"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/grafana/alloy/internal/component"
	"github.com/grafana/alloy/internal/component/common/spiffe"
	"github.com/grafana/alloy/internal/component/common/spiffe/spiffetest"
	lokiwrite "github.com/grafana/alloy/internal/component/loki/write"
	"github.com/grafana/alloy/internal/component/prometheus/remotewrite"
	"github.com/grafana/alloy/internal/featuregate"
	"github.com/grafana/alloy/internal/runtime"
	"github.com/grafana/alloy/internal/runtime/logging"
	"github.com/grafana/alloy/internal/service"
	"github.com/grafana/alloy/internal/service/labelstore"
	"github.com/grafana/alloy/internal/service/livedebugging"
)

// Endpoints receive the source through Alloy's config evaluation, which may copy capsule values.
func TestEndpointsShareSourceThroughRuntime(t *testing.T) {
	ca := spiffetest.NewCA(t, "example.org")
	api := spiffetest.NewWorkloadAPI(t)

	l, err := logging.New(io.Discard, logging.DefaultOptions)
	require.NoError(t, err)
	ctrl, err := runtime.New(runtime.Options{
		Logger:       l,
		DataPath:     t.TempDir(),
		MinStability: featuregate.StabilityExperimental,
		Services: []service.Service{
			labelstore.New(nil, prometheus.NewRegistry()),
			livedebugging.New(),
		},
	})
	require.NoError(t, err)

	src, err := runtime.ParseSource(t.Name(), []byte(fmt.Sprintf(`
		spiffe.x509_source "agent" {
			address = %q
		}
		prometheus.remote_write "mimir" {
			endpoint {
				url = "https://mimir.example/api/v1/push"
				spiffe {
					source     = spiffe.x509_source.agent.source
					server_ids = ["spiffe://example.org/mimir"]
				}
			}
		}
		loki.write "loki" {
			endpoint {
				url = "https://loki.example/loki/api/v1/push"
				spiffe {
					source     = spiffe.x509_source.agent.source
					server_ids = ["spiffe://example.org/loki"]
				}
			}
		}
	`, api.Addr())))
	require.NoError(t, err)
	require.NoError(t, ctrl.LoadSource(src, nil, ""))
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go ctrl.Run(ctx)

	api.SetX509SVID(t, ca, ca.SVID(t, "spiffe://example.org/alloy"))

	args := func(id string) component.Arguments {
		info, err := ctrl.GetComponent(component.ID{LocalID: id}, component.InfoOptions{GetArguments: true})
		require.NoError(t, err)
		return info.Arguments
	}
	sources := map[string]*spiffe.Source{
		"prometheus.remote_write": args("prometheus.remote_write.mimir").(remotewrite.Arguments).Endpoints[0].SPIFFE.Source,
		"loki.write":              args("loki.write.loki").(lokiwrite.Arguments).Endpoints[0].SPIFFE.Source,
	}
	for name, s := range sources {
		require.Eventually(t, func() bool { return currentID(s) == "spiffe://example.org/alloy" }, 10*time.Second, 50*time.Millisecond, name)
	}
}
