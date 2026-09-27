package spiffetest

import (
	"crypto/x509"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/spiffe/go-spiffe/v2/proto/spiffe/workload"
	"github.com/spiffe/go-spiffe/v2/svid/x509svid"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
)

// WorkloadAPI is a fake SPIFFE Workload API serving X.509 contexts on a Unix socket.
type WorkloadAPI struct {
	workload.UnimplementedSpiffeWorkloadAPIServer

	path string
	mut  sync.Mutex
	srv  *grpc.Server
	resp *workload.X509SVIDResponse
	subs map[chan *workload.X509SVIDResponse]struct{}
}

func NewWorkloadAPI(t testing.TB) *WorkloadAPI {
	t.Helper()
	// os.MkdirTemp rather than t.TempDir: macOS caps Unix socket paths at 104 bytes.
	dir, err := os.MkdirTemp("", "wlapi")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	w := &WorkloadAPI{
		path: filepath.Join(dir, "agent.sock"),
		subs: map[chan *workload.X509SVIDResponse]struct{}{},
	}
	w.Start(t)
	t.Cleanup(w.Stop)
	return w
}

func (w *WorkloadAPI) Addr() string { return "unix://" + w.path }

func (w *WorkloadAPI) Start(t testing.TB) {
	t.Helper()
	lis, err := net.Listen("unix", w.path)
	require.NoError(t, err)
	srv := grpc.NewServer()
	workload.RegisterSpiffeWorkloadAPIServer(srv, w)
	w.mut.Lock()
	w.srv = srv
	w.mut.Unlock()
	go func() { _ = srv.Serve(lis) }()
}

// Stop closes the listener and every open stream, as an agent restart would.
func (w *WorkloadAPI) Stop() {
	w.mut.Lock()
	srv := w.srv
	w.mut.Unlock()
	srv.Stop()
}

// SetX509SVID sends svid and ca's bundle to every open stream and to streams opened later.
func (w *WorkloadAPI) SetX509SVID(t testing.TB, ca *CA, svid *x509svid.SVID) {
	t.Helper()
	key, err := x509.MarshalPKCS8PrivateKey(svid.PrivateKey)
	require.NoError(t, err)
	var chain []byte
	for _, c := range svid.Certificates {
		chain = append(chain, c.Raw...)
	}
	resp := &workload.X509SVIDResponse{Svids: []*workload.X509SVID{{
		SpiffeId:    svid.ID.String(),
		X509Svid:    chain,
		X509SvidKey: key,
		Bundle:      ca.cert.Raw,
	}}}
	w.mut.Lock()
	defer w.mut.Unlock()
	w.resp = resp
	for ch := range w.subs {
		ch <- resp
	}
}

func (w *WorkloadAPI) FetchX509SVID(_ *workload.X509SVIDRequest, stream workload.SpiffeWorkloadAPI_FetchX509SVIDServer) error {
	ch := make(chan *workload.X509SVIDResponse, 8)
	w.mut.Lock()
	if w.resp != nil {
		ch <- w.resp
	}
	w.subs[ch] = struct{}{}
	w.mut.Unlock()
	defer func() {
		w.mut.Lock()
		delete(w.subs, ch)
		w.mut.Unlock()
	}()

	for {
		select {
		case <-stream.Context().Done():
			return nil
		case resp := <-ch:
			if err := stream.Send(resp); err != nil {
				return err
			}
		}
	}
}
