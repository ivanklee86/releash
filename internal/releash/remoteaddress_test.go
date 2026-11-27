package releash

import (
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Cases for the automatic remoteAddress from spec 0001:
//   - without --remote-address, an IP strategy listing the address releash
//     reaches Unleash from is true, over IPv4 and IPv6, and one listing
//     another address is false, with nothing on stderr even with --loud
//   - after a redirect, the address is that of the connection that served
//     the flags, even when the redirect changes the path
//   - --remote-address replaces the detected address in both directions
//   - the detected address is the plain IP: no port, no zone, and an IPv4
//     address mapped into IPv6 reads as IPv4; anything that is not an IP
//     address leaves it unset
//   - with no address, remoteAddress stays unset and a --loud note says so;
//     a given --remote-address needs no note
//
// Check cannot be made to find no local address against a TCP server, so the
// note is checked on withRemoteAddress only.

// byAddress is an enabled flag whose one strategy is remoteAddress with ips.
func byAddress(name, ips string) string {
	return fmt.Sprintf(`{"name":%q,"enabled":true,"strategies":[{"name":"remoteAddress","parameters":{"IPs":%q}}]}`, name, ips)
}

func TestCheckUsesTheLocalAddressAsRemoteAddress(t *testing.T) {
	features := serve(http.StatusOK, featuresJSON(
		byAddress("v4only", "127.0.0.1"),
		byAddress("v6only", "::1"),
		byAddress("elsewhere", "203.0.113.7"),
	))
	url := serveOnLoopback(t, features, "127.0.0.1:0") + "/api"

	tests := []struct {
		name       string
		flag       string
		edits      []func(*CheckOptions)
		wantStdout string
	}{
		{"detected address listed", "v4only", nil, "true\n"},
		{"other loopback family listed", "v6only", nil, "false\n"},
		{"detected address not listed", "elsewhere", nil, "false\n"},
		{"--remote-address outside the list", "v4only", []func(*CheckOptions){set(OptionRemoteAddress, remoteAddress, "203.0.113.8")}, "false\n"},
		{"--remote-address in the list", "elsewhere", []func(*CheckOptions){set(OptionRemoteAddress, remoteAddress, "203.0.113.7")}, "true\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, loud := range []bool{false, true} {
				stdout, stderr, code := runCheck(t, url, tt.flag, loud, tt.edits...)
				assert.Equal(t, tt.wantStdout, stdout, "loud=%t", loud)
				assert.Empty(t, stderr, "loud=%t", loud)
				assert.Equal(t, ExitOK, code, "loud=%t", loud)
			}
		})
	}

	t.Run("IPv6", func(t *testing.T) {
		v6 := serveOnLoopback(t, features, "[::1]:0")
		for flag, want := range map[string]string{"v6only": "true\n", "v4only": "false\n"} {
			stdout, stderr, code := runCheck(t, v6+"/api", flag, true)
			assert.Equal(t, want, stdout, flag)
			assert.Empty(t, stderr, flag)
			assert.Equal(t, ExitOK, code, flag)
		}
	})
}

// A redirect may send the fetch to a server with another path and address;
// the flags come over the connection to that server, so its address counts.
func TestCheckUsesTheAddressOfTheRedirectTarget(t *testing.T) {
	mirror := serveOnLoopback(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/mirror/flags" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		serve(http.StatusOK, featuresJSON(byAddress("v6only", "::1"), byAddress("v4only", "127.0.0.1")))(w, r)
	}, "[::1]:0")
	url := serveOnLoopback(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/client/features" {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		http.Redirect(w, r, mirror+"/mirror/flags", http.StatusTemporaryRedirect)
	}, "127.0.0.1:0") + "/api"

	for flag, want := range map[string]string{"v6only": "true\n", "v4only": "false\n"} {
		stdout, stderr, code := runCheck(t, url, flag, true)
		assert.Equal(t, want, stdout, flag)
		assert.Empty(t, stderr, flag)
		assert.Equal(t, ExitOK, code, flag)
	}
}

// serveOnLoopback serves handler on addr, a loopback address with port 0,
// and returns its URL. It skips the test when addr is IPv6 and the host has
// no IPv6 loopback, and fails it when the host cannot listen on IPv4.
func serveOnLoopback(t *testing.T, handler http.HandlerFunc, addr string) string {
	t.Helper()
	ln, err := net.Listen("tcp", addr)
	if err != nil && strings.HasPrefix(addr, "[") {
		t.Skipf("cannot listen on %s: %v", addr, err)
	}
	require.NoError(t, err, "listening on %s", addr)
	srv := httptest.NewUnstartedServer(handler)
	_ = srv.Listener.Close()
	srv.Listener = ln
	srv.Start()
	t.Cleanup(srv.Close)
	return srv.URL
}

func TestLocalAddressKeepsThePlainIP(t *testing.T) {
	unix := &net.UnixAddr{Name: "/run/unleash.sock", Net: "unix"}
	tests := []struct {
		name string
		addr net.Addr
		want string
	}{
		{"IPv4", &net.TCPAddr{IP: net.ParseIP("10.1.2.3"), Port: 50000}, "10.1.2.3"},
		{"IPv6", &net.TCPAddr{IP: net.ParseIP("fd00::1"), Port: 50000}, "fd00::1"},
		{"IPv4 mapped into IPv6", &net.TCPAddr{IP: net.ParseIP("::ffff:10.1.2.3"), Port: 50000}, "10.1.2.3"},
		{"IPv6 with a zone", &net.TCPAddr{IP: net.ParseIP("fe80::1"), Port: 50000, Zone: "eth0"}, "fe80::1"},
		{"unspecified", &net.TCPAddr{IP: net.IPv4zero, Port: 50000}, ""},
		{"Unix socket", unix, ""},
		{"nil", nil, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := &localAddress{}
			l.record(tt.addr)
			assert.Equal(t, tt.want, l.address())
		})
	}

	t.Run("a later connection replaces an earlier one", func(t *testing.T) {
		l := &localAddress{}
		l.record(&net.TCPAddr{IP: net.ParseIP("10.1.2.3"), Port: 1})
		l.record(unix)
		assert.Empty(t, l.address())
	})
}

func TestWithRemoteAddress(t *testing.T) {
	tests := []struct {
		name     string
		given    string
		detected string
		want     string
		wantNote string
	}{
		{"detected fills an unset address", "", "10.1.2.3", "10.1.2.3", ""},
		{"given wins over detected", "203.0.113.7", "10.1.2.3", "203.0.113.7", ""},
		{"given needs no detected address", "203.0.113.7", "", "203.0.113.7", ""},
		{"neither leaves it unset with a note", "", "", "", noLocalAddress},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, note := withRemoteAddress(evalContext{remoteAddress: tt.given, appName: "pipeline"}, tt.detected)
			assert.Equal(t, tt.want, c.remoteAddress)
			assert.Equal(t, "pipeline", c.appName, "other fields kept")
			assert.Equal(t, tt.wantNote, note)
		})
	}
}
