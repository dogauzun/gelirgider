package ratelimit

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestLimit(t *testing.T) {
	const n = 10
	const window = 50 * time.Millisecond

	ok := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := Limit(n, window)(ok)

	port := 1234
	req := func() *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodPost, "/", nil)
		// Same host, new ephemeral port per request, as with real TCP connections.
		r.RemoteAddr = fmt.Sprintf("10.0.0.1:%d", port)
		port++
		handler.ServeHTTP(rec, r)
		return rec
	}

	for i := 0; i < n; i++ {
		if rec := req(); rec.Code != http.StatusOK {
			t.Fatalf("request %d: status = %d, want %d", i+1, rec.Code, http.StatusOK)
		}
	}

	rec := req()
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("11th request: status = %d, want %d", rec.Code, http.StatusTooManyRequests)
	}
	if rec.Header().Get("Retry-After") == "" {
		t.Fatal("11th request: missing Retry-After header")
	}

	time.Sleep(2 * window)

	if rec := req(); rec.Code != http.StatusOK {
		t.Fatalf("after window elapsed: status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestClientIPTrustedHops(t *testing.T) {
	TrustedProxyHops = 1
	t.Cleanup(func() { TrustedProxyHops = 0 })

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "10.0.0.2:4321"
	// The client sent 1.2.3.4 itself; the one trusted proxy appended the
	// address it saw.
	r.Header.Set("X-Forwarded-For", "1.2.3.4, 203.0.113.9")
	if got := ClientIP(r); got != "203.0.113.9" {
		t.Fatalf("ClientIP = %q, want %q", got, "203.0.113.9")
	}
}
