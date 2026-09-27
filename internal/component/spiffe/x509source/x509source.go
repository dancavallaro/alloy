// Package x509source provides the spiffe.x509_source component.
package x509source

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/spiffe/go-spiffe/v2/workloadapi"

	"github.com/grafana/alloy/internal/component"
	"github.com/grafana/alloy/internal/component/common/spiffe"
	"github.com/grafana/alloy/internal/featuregate"
)

// retryInterval applies only when go-spiffe gives up on a watch; it retries transient failures itself.
const retryInterval = 5 * time.Second

func init() {
	component.Register(component.Registration{
		Name:      "spiffe.x509_source",
		Stability: featuregate.StabilityExperimental,
		Args:      Arguments{},
		Exports:   Exports{},
		Build: func(opts component.Options, args component.Arguments) (component.Component, error) {
			return New(opts, args.(Arguments))
		},
	})
}

type Arguments struct {
	Address string `alloy:"address,attr,optional"`
}

func (a *Arguments) Validate() error {
	addr := a.resolvedAddress()
	if addr == "" {
		return errors.New("address must be set when SPIFFE_ENDPOINT_SOCKET is unset")
	}
	return workloadapi.ValidateAddress(addr)
}

func (a Arguments) resolvedAddress() string {
	if a.Address != "" {
		return a.Address
	}
	addr, _ := workloadapi.GetDefaultAddress()
	return addr
}

type Exports struct {
	Source *spiffe.Source `alloy:"source,attr"`
}

type Component struct {
	opts    component.Options
	source  *spiffe.Source
	restart chan struct{}

	mut     sync.Mutex
	address string
}

var (
	_ component.Component       = (*Component)(nil)
	_ component.HealthComponent = (*Component)(nil)
	_ component.DebugComponent  = (*Component)(nil)
)

func New(opts component.Options, args Arguments) (*Component, error) {
	c := &Component{
		opts:    opts,
		source:  spiffe.NewSource(),
		restart: make(chan struct{}, 1),
		address: args.resolvedAddress(),
	}
	opts.OnStateChange(Exports{Source: c.source})
	return c, nil
}

func (c *Component) Run(ctx context.Context) error {
	for {
		c.mut.Lock()
		addr := c.address
		c.mut.Unlock()

		watchCtx, cancel := context.WithCancel(ctx)
		done := make(chan struct{})
		go func() {
			defer close(done)
			c.watch(watchCtx, addr)
		}()

		select {
		case <-ctx.Done():
			cancel()
			<-done
			return nil
		case <-c.restart:
			cancel()
			<-done
		}
	}
}

func (c *Component) watch(ctx context.Context, addr string) {
	for {
		err := c.watchOnce(ctx, addr)
		if ctx.Err() != nil {
			return
		}
		c.source.OnX509ContextWatchError(err)
		c.opts.Logger.Warn("Workload API watch ended, retrying", "address", addr, "err", err)
		select {
		case <-ctx.Done():
			return
		case <-time.After(retryInterval):
		}
	}
}

func (c *Component) watchOnce(ctx context.Context, addr string) error {
	client, err := workloadapi.New(ctx, workloadapi.WithAddr(addr), workloadapi.WithLogger(logAdapter{c.opts.Logger}))
	if err != nil {
		return err
	}
	defer client.Close()
	return client.WatchX509Context(ctx, c.source)
}

func (c *Component) Update(args component.Arguments) error {
	addr := args.(Arguments).resolvedAddress()
	c.mut.Lock()
	defer c.mut.Unlock()
	if addr == c.address {
		return nil
	}
	c.address = addr
	select {
	case c.restart <- struct{}{}:
	default:
	}
	return nil
}

func (c *Component) CurrentHealth() component.Health {
	st := c.source.Status()
	switch {
	case !st.Ready:
		msg := "waiting for the first X509-SVID"
		if st.WatchErr != nil {
			msg += ": " + st.WatchErr.Error()
		}
		return component.Health{Health: component.HealthTypeUnhealthy, Message: msg, UpdateTime: time.Now()}
	case time.Now().After(st.NotAfter):
		return component.Health{
			Health:     component.HealthTypeUnhealthy,
			Message:    fmt.Sprintf("X509-SVID %s expired at %s", st.SPIFFEID, st.NotAfter.Format(time.RFC3339)),
			UpdateTime: time.Now(),
		}
	case st.WatchErr != nil:
		return component.Health{
			Health:     component.HealthTypeUnhealthy,
			Message:    "Workload API watch failing, serving cached X509-SVID: " + st.WatchErr.Error(),
			UpdateTime: time.Now(),
		}
	default:
		return component.Health{
			Health:     component.HealthTypeHealthy,
			Message:    fmt.Sprintf("serving %s, expires %s", st.SPIFFEID, st.NotAfter.Format(time.RFC3339)),
			UpdateTime: st.UpdatedAt,
		}
	}
}

type debugInfo struct {
	SPIFFEID     string   `alloy:"spiffe_id,attr,optional"`
	NotAfter     string   `alloy:"not_after,attr,optional"`
	TrustDomains []string `alloy:"trust_domains,attr,optional"`
}

func (c *Component) DebugInfo() any {
	st := c.source.Status()
	if !st.Ready {
		return debugInfo{}
	}
	return debugInfo{
		SPIFFEID:     st.SPIFFEID,
		NotAfter:     st.NotAfter.Format(time.RFC3339),
		TrustDomains: st.TrustDomains,
	}
}
