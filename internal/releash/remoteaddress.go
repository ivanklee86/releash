package releash

import (
	"net"
	"net/http"
	"net/http/httptrace"
	"net/netip"
	"strings"
	"sync"
)

// noLocalAddress is the --loud note for a check that found no local address
// to use as remoteAddress.
const noLocalAddress = "could not find the local address of the connection to unleash; remoteAddress is unset"

// localAddress keeps the local IP address of the connection that carried the
// latest feature fetch. It is safe for concurrent use.
type localAddress struct {
	mu   sync.Mutex
	addr string
}

// record stores the IP address of a as the local address, or clears it when
// a is not an IP address. An IPv4 address mapped into IPv6 is stored as IPv4
// and a zone is dropped, because Unleash compares the plain address with its
// IP lists, and --remote-address does not accept zones either.
func (l *localAddress) record(a net.Addr) {
	addr := ""
	if ap, err := netip.ParseAddrPort(addrString(a)); err == nil && ap.Addr().IsValid() && !ap.Addr().IsUnspecified() {
		addr = ap.Addr().Unmap().WithZone("").String()
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.addr = addr
}

func addrString(a net.Addr) string {
	if a == nil {
		return ""
	}
	return a.String()
}

// address returns the recorded address, or "" if there is none.
func (l *localAddress) address() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.addr
}

// localAddressTransport sends requests through base and records in addr the
// local address of the connection each feature fetch uses, including each
// hop of a redirected one, whatever its path. It must sit under
// boundedTransport, which links each redirect hop to the one before it.
type localAddressTransport struct {
	base http.RoundTripper
	addr *localAddress
}

func (t *localAddressTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if !strings.HasSuffix(redirectOrigin(req).URL.Path, featuresPath) {
		return t.base.RoundTrip(req)
	}
	trace := &httptrace.ClientTrace{
		GotConn: func(info httptrace.GotConnInfo) { t.addr.record(info.Conn.LocalAddr()) },
	}
	return t.base.RoundTrip(req.WithContext(httptrace.WithClientTrace(req.Context(), trace)))
}

// withRemoteAddress returns c with remoteAddress set to detected when it is
// unset, and the --loud note to add when neither gives an address.
func withRemoteAddress(c evalContext, detected string) (evalContext, string) {
	if c.remoteAddress != "" {
		return c, ""
	}
	if detected == "" {
		return c, noLocalAddress
	}
	c.remoteAddress = detected
	return c, ""
}
