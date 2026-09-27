package spiffe_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/alloy/internal/component/common/spiffe"
	"github.com/grafana/alloy/internal/component/common/spiffe/spiffetest"
)

func TestSourceBeforeFirstUpdate(t *testing.T) {
	s := spiffe.NewSource()
	_, err := s.GetX509SVID()
	require.ErrorIs(t, err, spiffe.ErrNoSVID)
	_, err = s.GetX509BundleForTrustDomain(spiffetest.NewCA(t, "example.org").TrustDomain)
	require.ErrorIs(t, err, spiffe.ErrNoSVID)
	require.False(t, s.Status().Ready)
}

func TestSourceUpdate(t *testing.T) {
	ca := spiffetest.NewCA(t, "example.org")
	svid := ca.SVID(t, "spiffe://example.org/alloy")
	s := spiffe.NewSource()
	s.OnX509ContextUpdate(ca.X509Context(svid))

	got, err := s.GetX509SVID()
	require.NoError(t, err)
	require.Equal(t, svid.ID, got.ID)

	b, err := s.GetX509BundleForTrustDomain(ca.TrustDomain)
	require.NoError(t, err)
	require.Equal(t, ca.TrustDomain, b.TrustDomain())

	st := s.Status()
	require.True(t, st.Ready)
	require.Equal(t, "spiffe://example.org/alloy", st.SPIFFEID)
	require.Equal(t, []string{"example.org"}, st.TrustDomains)
	require.Equal(t, svid.Certificates[0].NotAfter, st.NotAfter)
	require.NoError(t, st.WatchErr)
}

func TestSourceWatchErrorKeepsCachedSVID(t *testing.T) {
	ca := spiffetest.NewCA(t, "example.org")
	s := spiffe.NewSource()
	s.OnX509ContextUpdate(ca.X509Context(ca.SVID(t, "spiffe://example.org/alloy")))

	s.OnX509ContextWatchError(errors.New("connection refused"))
	_, err := s.GetX509SVID()
	require.NoError(t, err)
	require.ErrorContains(t, s.Status().WatchErr, "connection refused")

	s.OnX509ContextUpdate(ca.X509Context(ca.SVID(t, "spiffe://example.org/alloy")))
	require.NoError(t, s.Status().WatchErr)
}

func TestSourceIgnoresCancellation(t *testing.T) {
	s := spiffe.NewSource()
	s.OnX509ContextWatchError(context.Canceled)
	require.NoError(t, s.Status().WatchErr)
}
