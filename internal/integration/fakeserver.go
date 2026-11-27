//go:build integration

package integration

import (
	"errors"
	"net"
	"sync"
	"testing"
)

// UnresponsiveServer starts a TCP server on the loopback address that accepts
// connections and never responds, and returns its URL (http://127.0.0.1:port).
// Requests to it hang until the client gives up, which is how a stuck Unleash
// looks to releash. The server and every connection it accepted are closed
// when the test finishes.
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
