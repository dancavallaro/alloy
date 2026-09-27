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
	// Alloy's decoder copies capsules whose type contains an interface, so all state lives behind this pointer.
	st *sourceState
}

type sourceState struct {
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

func NewSource() *Source { return &Source{st: &sourceState{}} }

func (*Source) AlloyCapsule() {}

func (s *Source) GetX509SVID() (*x509svid.SVID, error) {
	s.st.mut.RLock()
	defer s.st.mut.RUnlock()
	if s.st.svid == nil {
		return nil, ErrNoSVID
	}
	return s.st.svid, nil
}

func (s *Source) GetX509BundleForTrustDomain(td spiffeid.TrustDomain) (*x509bundle.Bundle, error) {
	s.st.mut.RLock()
	defer s.st.mut.RUnlock()
	if s.st.bundles == nil {
		return nil, ErrNoSVID
	}
	return s.st.bundles.GetX509BundleForTrustDomain(td)
}

func (s *Source) OnX509ContextUpdate(c *workloadapi.X509Context) {
	s.st.mut.Lock()
	defer s.st.mut.Unlock()
	s.st.svid = c.DefaultSVID()
	s.st.bundles = c.Bundles
	s.st.updatedAt = time.Now()
	s.st.watchErr = nil
}

func (s *Source) OnX509ContextWatchError(err error) {
	// Cancellation is how the component stops a watch, not a failure.
	if errors.Is(err, context.Canceled) || status.Code(err) == codes.Canceled {
		return
	}
	s.st.mut.Lock()
	defer s.st.mut.Unlock()
	s.st.watchErr = err
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
	s.st.mut.RLock()
	defer s.st.mut.RUnlock()
	st := Status{UpdatedAt: s.st.updatedAt, WatchErr: s.st.watchErr}
	if s.st.svid == nil {
		return st
	}
	st.Ready = true
	st.SPIFFEID = s.st.svid.ID.String()
	st.NotAfter = s.st.svid.Certificates[0].NotAfter
	for _, b := range s.st.bundles.Bundles() {
		st.TrustDomains = append(st.TrustDomains, b.TrustDomain().String())
	}
	sort.Strings(st.TrustDomains)
	return st
}
