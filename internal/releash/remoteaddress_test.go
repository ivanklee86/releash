package releash

import (
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

// Cases for the automatic remoteAddress from spec 0001:
//   - without --remote-address, an IP strategy listing the address releash
//     reaches Unleash from is true, over IPv4 and IPv6, and one listing
//     another address is false, with nothing on stderr even with --loud
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
		byAddress("loopback", "127.0.0.1, ::1"),
		byAddress("elsewhere", "203.0.113.7"),
	))
	url := fakeUnleash(t, features, nil)

	tests := []struct {
		name       string
		flag       string
		edits      []func(*CheckOptions)
		wantStdout string
	}{
		{"detected address listed", "loopback", nil, "true\n"},
		{"detected address not listed", "elsewhere", nil, "false\n"},
		{"--remote-address outside the list", "loopback", []func(*CheckOptions){set(OptionRemoteAddress, remoteAddress, "203.0.113.8")}, "false\n"},
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
		ln, err := net.Listen("tcp", "[::1]:0")
		if err != nil {
			t.Skipf("no IPv6 loopback: %v", err)
		}
		srv := httptest.NewUnstartedServer(features)
		_ = srv.Listener.Close()
		srv.Listener = ln
		srv.Start()
		t.Cleanup(srv.Close)

		stdout, stderr, _ := runCheck(t, srv.URL+"/api", "loopback", true)
		assert.Equal(t, "true\n", stdout)
		assert.Empty(t, stderr)
	})
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
