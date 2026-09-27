package x509source_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/grafana/alloy/internal/component"
	"github.com/grafana/alloy/internal/component/common/spiffe"
	"github.com/grafana/alloy/internal/component/common/spiffe/spiffetest"
	"github.com/grafana/alloy/internal/component/spiffe/x509source"
	"github.com/grafana/alloy/internal/runtime/componenttest"
	"github.com/grafana/alloy/internal/util"
)

func run(t *testing.T, args x509source.Arguments) (*componenttest.Controller, *spiffe.Source) {
	t.Helper()
	tc, err := componenttest.NewControllerFromID(util.TestLogger(t), "spiffe.x509_source")
	require.NoError(t, err)
	go func() { require.NoError(t, tc.Run(componenttest.TestContext(t), args)) }()
	require.NoError(t, tc.WaitExports(5*time.Second))
	// WaitExports fires from New, before the controller stores the component; WaitRunning fires after.
	require.NoError(t, tc.WaitRunning(5*time.Second))
	return tc, tc.Exports().(x509source.Exports).Source
}

func health(t *testing.T, tc *componenttest.Controller) component.Health {
	t.Helper()
	c, err := tc.GetComponent()
	require.NoError(t, err)
	return c.(component.HealthComponent).CurrentHealth()
}

func currentID(src *spiffe.Source) string {
	svid, err := src.GetX509SVID()
	if err != nil {
		return ""
	}
	return svid.ID.String()
}

func TestReceivesSVID(t *testing.T) {
	ca := spiffetest.NewCA(t, "example.org")
	api := spiffetest.NewWorkloadAPI(t)
	api.SetX509SVID(t, ca, ca.SVID(t, "spiffe://example.org/alloy"))

	tc, src := run(t, x509source.Arguments{Address: api.Addr()})
	require.Eventually(t, func() bool { return currentID(src) == "spiffe://example.org/alloy" }, 10*time.Second, 50*time.Millisecond)
	h := health(t, tc)
	require.Equal(t, component.HealthTypeHealthy, h.Health)
	require.Contains(t, h.Message, "spiffe://example.org/alloy")
}

func TestUnhealthyBeforeFirstSVID(t *testing.T) {
	api := spiffetest.NewWorkloadAPI(t)
	tc, _ := run(t, x509source.Arguments{Address: api.Addr()})
	require.Equal(t, component.HealthTypeUnhealthy, health(t, tc).Health)
}

func TestRotation(t *testing.T) {
	ca := spiffetest.NewCA(t, "example.org")
	api := spiffetest.NewWorkloadAPI(t)
	api.SetX509SVID(t, ca, ca.SVID(t, "spiffe://example.org/alloy"))
	_, src := run(t, x509source.Arguments{Address: api.Addr()})
	require.Eventually(t, func() bool { return currentID(src) == "spiffe://example.org/alloy" }, 10*time.Second, 50*time.Millisecond)

	api.SetX509SVID(t, ca, ca.SVID(t, "spiffe://example.org/alloy-rotated"))
	require.Eventually(t, func() bool { return currentID(src) == "spiffe://example.org/alloy-rotated" }, 10*time.Second, 50*time.Millisecond)
}

func TestAgentRestart(t *testing.T) {
	ca := spiffetest.NewCA(t, "example.org")
	api := spiffetest.NewWorkloadAPI(t)
	api.SetX509SVID(t, ca, ca.SVID(t, "spiffe://example.org/alloy"))
	tc, src := run(t, x509source.Arguments{Address: api.Addr()})
	require.Eventually(t, func() bool { return health(t, tc).Health == component.HealthTypeHealthy }, 10*time.Second, 50*time.Millisecond)

	api.Stop()
	require.Eventually(t, func() bool { return health(t, tc).Health == component.HealthTypeUnhealthy }, 10*time.Second, 50*time.Millisecond)
	require.Equal(t, "spiffe://example.org/alloy", currentID(src), "cached SVID must keep serving while the agent is down")

	api.Start(t)
	require.Eventually(t, func() bool { return health(t, tc).Health == component.HealthTypeHealthy }, 45*time.Second, 100*time.Millisecond)
}

func TestAddressChange(t *testing.T) {
	ca := spiffetest.NewCA(t, "example.org")
	first := spiffetest.NewWorkloadAPI(t)
	first.SetX509SVID(t, ca, ca.SVID(t, "spiffe://example.org/first"))
	second := spiffetest.NewWorkloadAPI(t)
	second.SetX509SVID(t, ca, ca.SVID(t, "spiffe://example.org/second"))

	tc, src := run(t, x509source.Arguments{Address: first.Addr()})
	require.Eventually(t, func() bool { return currentID(src) == "spiffe://example.org/first" }, 10*time.Second, 50*time.Millisecond)

	require.NoError(t, tc.Update(x509source.Arguments{Address: second.Addr()}))
	require.Eventually(t, func() bool { return currentID(src) == "spiffe://example.org/second" }, 10*time.Second, 50*time.Millisecond)
	require.Same(t, src, tc.Exports().(x509source.Exports).Source, "export must not change on Update")
}

func TestValidate(t *testing.T) {
	t.Setenv("SPIFFE_ENDPOINT_SOCKET", "")
	require.ErrorContains(t, (&x509source.Arguments{}).Validate(), "address")
	require.Error(t, (&x509source.Arguments{Address: "foo://bar"}).Validate())
	require.NoError(t, (&x509source.Arguments{Address: "unix:///run/spire/agent.sock"}).Validate())

	t.Setenv("SPIFFE_ENDPOINT_SOCKET", "unix:///run/spire/agent.sock")
	require.NoError(t, (&x509source.Arguments{}).Validate())
}

func TestUnhealthyWhenSVIDExpired(t *testing.T) {
	ca := spiffetest.NewCA(t, "example.org")
	api := spiffetest.NewWorkloadAPI(t)
	api.SetX509SVID(t, ca, ca.ExpiredSVID(t, "spiffe://example.org/alloy"))

	tc, src := run(t, x509source.Arguments{Address: api.Addr()})
	require.Eventually(t, func() bool { return currentID(src) == "spiffe://example.org/alloy" }, 10*time.Second, 50*time.Millisecond)
	h := health(t, tc)
	require.Equal(t, component.HealthTypeUnhealthy, h.Health)
	require.Contains(t, h.Message, "expired")
}
