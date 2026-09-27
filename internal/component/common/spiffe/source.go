// Package spiffe holds the SPIFFE X.509 source shared by spiffe.x509_source and the endpoints that use it.
package spiffe

import (
	"context"
	"errors"
	"sort"
	"sync"
	"time"

	"github.com/spiffe/go-spiffe/v2/bundle/x509bundle"
	"github.com/spiffe/go-spiffe/v2/spiffeid"
	"github.com/spiffe/go-spiffe/v2/svid/x509svid"
	"github.com/spiffe/go-spiffe/v2/workloadapi"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

var ErrNoSVID = errors.New("no X509-SVID received from the Workload API yet")

// Source holds the latest X.509 context from the Workload API and is exported to Alloy as a capsule.
type Source struct {
	mut       sync.RWMutex
	svid      *x509svid.SVID
	bundles   *x509bundle.Set
	updatedAt time.Time
	watchErr  error
}

var (
	_ x509svid.Source                = (*Source)(nil)
	_ x509bundle.Source              = (*Source)(nil)
	_ workloadapi.X509ContextWatcher = (*Source)(nil)
)

func NewSource() *Source { return &Source{} }

func (*Source) AlloyCapsule() {}

func (s *Source) GetX509SVID() (*x509svid.SVID, error) {
	s.mut.RLock()
	defer s.mut.RUnlock()
	if s.svid == nil {
		return nil, ErrNoSVID
	}
	return s.svid, nil
}

func (s *Source) GetX509BundleForTrustDomain(td spiffeid.TrustDomain) (*x509bundle.Bundle, error) {
	s.mut.RLock()
	defer s.mut.RUnlock()
	if s.bundles == nil {
		return nil, ErrNoSVID
	}
	return s.bundles.GetX509BundleForTrustDomain(td)
}

func (s *Source) OnX509ContextUpdate(c *workloadapi.X509Context) {
	s.mut.Lock()
	defer s.mut.Unlock()
	s.svid = c.DefaultSVID()
	s.bundles = c.Bundles
	s.updatedAt = time.Now()
	s.watchErr = nil
}

func (s *Source) OnX509ContextWatchError(err error) {
	// Cancellation is how the component stops a watch, not a failure.
	if errors.Is(err, context.Canceled) || status.Code(err) == codes.Canceled {
		return
	}
	s.mut.Lock()
	defer s.mut.Unlock()
	s.watchErr = err
}

type Status struct {
	Ready        bool
	SPIFFEID     string
	NotAfter     time.Time
	TrustDomains []string
	UpdatedAt    time.Time
	WatchErr     error
}

func (s *Source) Status() Status {
	s.mut.RLock()
	defer s.mut.RUnlock()
	st := Status{UpdatedAt: s.updatedAt, WatchErr: s.watchErr}
	if s.svid == nil {
		return st
	}
	st.Ready = true
	st.SPIFFEID = s.svid.ID.String()
	st.NotAfter = s.svid.Certificates[0].NotAfter
	for _, b := range s.bundles.Bundles() {
		st.TrustDomains = append(st.TrustDomains, b.TrustDomain().String())
	}
	sort.Strings(st.TrustDomains)
	return st
}
