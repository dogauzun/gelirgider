package server

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"testing"
)

// errExternalHost is what a request to a non-loopback host gets in this
// package's tests.
var errExternalHost = errors.New("external host blocked in tests")

// loopbackOnly lets requests to httptest servers through and fails every
// other one, so no test reaches a real host.
type loopbackOnly struct{ next http.RoundTripper }

func (t loopbackOnly) RoundTrip(req *http.Request) (*http.Response, error) {
	host := req.URL.Hostname()
	if ip := net.ParseIP(host); host != "localhost" && (ip == nil || !ip.IsLoopback()) {
		return nil, fmt.Errorf("%w: %s", errExternalHost, host)
	}
	return t.next.RoundTrip(req)
}

// TestMain blocks external hosts for every test in the package (GLG-395).
// New wires the real fetchers, which default to http.DefaultTransport, so
// every transaction save or import commit in a test built on New queues an
// on-demand backfill that would otherwise fetch from borsaistanbul.com and
// tcmb.gov.tr. Blocked, the walk fails on its first request.
func TestMain(m *testing.M) {
	http.DefaultTransport = loopbackOnly{next: http.DefaultTransport}
	os.Exit(m.Run())
}

func TestExternalHostBlocked(t *testing.T) {
	_, err := http.Get("https://www.borsaistanbul.com/data/thb/")
	if !errors.Is(err, errExternalHost) {
		t.Fatalf("request to external host: got %v, want %v", err, errExternalHost)
	}
}
