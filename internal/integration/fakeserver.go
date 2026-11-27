//go:build integration

package integration

import (
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

// FakeUnleash describes a fake Client API for the failures the local stack
// cannot produce on demand, such as a malformed response or a rejected
// register request. Start serves it.
type FakeUnleash struct {
	// FeaturesStatus answers GET /api/client/features. Zero means 200.
	FeaturesStatus int
	// FeaturesBody is the body of that answer, sent as is.
	FeaturesBody string
	// RegisterStatus answers POST /api/client/register. Zero means 202.
	RegisterStatus int
	// MetricsStatus answers POST /api/client/metrics. Zero means 202.
	MetricsStatus int
	// MetricsHang makes POST /api/client/metrics never answer, until the
	// client gives up or the test ends. It overrides MetricsStatus.
	MetricsHang bool
}

// EnabledFlagBody returns a Client API response holding one flag, name,
// switched on for everyone.
func EnabledFlagBody(name string) string {
	return fmt.Sprintf(`{"version":2,"features":[{"name":%q,"enabled":true,"strategies":[{"name":"default"}]}]}`, name)
}

// Start starts the fake on the loopback address and returns its API URL,
// with /api. Every other path answers 202. The server is closed when the test
// finishes.
func (f FakeUnleash) Start(t testing.TB) string {
	t.Helper()
	stop := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/client/features":
			w.WriteHeader(orDefault(f.FeaturesStatus, http.StatusOK))
			_, _ = io.WriteString(w, f.FeaturesBody)
		case "/api/client/register":
			w.WriteHeader(orDefault(f.RegisterStatus, http.StatusAccepted))
		case "/api/client/metrics":
			if f.MetricsHang {
				// The server notices a client giving up only once the body is read.
				_, _ = io.Copy(io.Discard, r.Body)
				select {
				case <-r.Context().Done():
				case <-stop:
				}
				return
			}
			w.WriteHeader(orDefault(f.MetricsStatus, http.StatusAccepted))
		default:
			w.WriteHeader(http.StatusAccepted)
		}
	}))
	// Cleanups run last first, so hanging handlers return before Close waits
	// for them.
	t.Cleanup(srv.Close)
	t.Cleanup(func() { close(stop) })
	return srv.URL + "/api"
}

// UnreachableURL returns an API URL on a loopback port that nothing listens
// on, so connecting to it is refused at once.
func UnreachableURL(t testing.TB) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("finding a free port: %v", err)
	}
	addr := ln.Addr().String()
	if err := ln.Close(); err != nil {
		t.Fatalf("freeing port %s: %v", addr, err)
	}
	return "http://" + addr + "/api"
}

func orDefault(status, fallback int) int {
	if status == 0 {
		return fallback
	}
	return status
}

// UnresponsiveServer starts a TCP server on the loopback address that accepts
// connections and never responds, and returns its URL (http://127.0.0.1:port).
// Requests to it hang until the client gives up, which is how a stuck Unleash
// looks to releash. Callers must give their request a client timeout or a
// context deadline: the server and every connection it accepted are closed only
// when the test finishes, so a request without one hangs until go test's own
// timeout.
func UnresponsiveServer(t testing.TB) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("starting unresponsive server: %v", err)
	}

	var (
		mu    sync.Mutex
		conns []net.Conn
		wg    sync.WaitGroup
	)
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			conn, err := ln.Accept()
			if err != nil {
				if !errors.Is(err, net.ErrClosed) {
					t.Errorf("unresponsive server: accept: %v", err)
				}
				return
			}
			mu.Lock()
			conns = append(conns, conn)
			mu.Unlock()
		}
	}()

	t.Cleanup(func() {
		_ = ln.Close()
		wg.Wait()
		mu.Lock()
		defer mu.Unlock()
		for _, c := range conns {
			_ = c.Close()
		}
	})
	return "http://" + ln.Addr().String()
}
