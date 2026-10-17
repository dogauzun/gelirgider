// Package ratelimit provides simple in-memory per-IP rate limiting
// middleware for HTTP handlers.
package ratelimit

import (
	"encoding/json"
	"math"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// TrustedProxyHops is the number of reverse proxies in front of the server
// that each append the address they saw to X-Forwarded-For. When above zero,
// ClientIP takes the X-Forwarded-For entry that many places from the right,
// the address the outermost trusted proxy saw; entries left of it are
// client-supplied and ignored. Zero means no trusted proxy: ClientIP uses
// http.Request.RemoteAddr. Set once at startup from config.Config.
var TrustedProxyHops int

type entry struct {
	count       int
	windowStart time.Time
}

// Limiter allows at most n hits per key within a rolling window of length
// per (a fixed-window counter). Use it directly where a handler counts only
// some of its requests, or one budget is shared by several routes.
//
// shortcut: in-memory limiter, single instance only — swap for a shared
// store (Redis) if the backend is ever scaled horizontally.
type Limiter struct {
	n    int
	per  time.Duration
	mu   sync.Mutex
	hits map[string]*entry
}

// New returns a Limiter of n hits per key a window of length per.
func New(n int, per time.Duration) *Limiter {
	l := &Limiter{n: n, per: per, hits: make(map[string]*entry)}
	// Lazily sweep stale keys so the map doesn't grow unbounded.
	go func() {
		ticker := time.NewTicker(per)
		defer ticker.Stop()
		for now := range ticker.C {
			l.mu.Lock()
			for k, e := range l.hits {
				if now.Sub(e.windowStart) >= per {
					delete(l.hits, k)
				}
			}
			l.mu.Unlock()
		}
	}()
	return l
}

// Allow counts one hit for key and reports whether it is within the limit;
// retryAfter is the time until key's window resets.
func (l *Limiter) Allow(key string) (ok bool, retryAfter time.Duration) {
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()
	e, found := l.hits[key]
	if !found || now.Sub(e.windowStart) >= l.per {
		e = &entry{windowStart: now}
		l.hits[key] = e
	}
	e.count++
	return e.count <= l.n, l.per - now.Sub(e.windowStart)
}

// Refund takes back one hit Allow counted for key, for a hit that turned out
// to cost nothing.
func (l *Limiter) Refund(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if e, found := l.hits[key]; found && e.count > 0 {
		e.count--
	}
}

// WriteTooMany writes the 429 JSON response of a request over the limit,
// with a Retry-After header giving the seconds until the window resets.
func WriteTooMany(w http.ResponseWriter, retryAfter time.Duration) {
	w.Header().Set("Retry-After", strconv.Itoa(int(math.Ceil(retryAfter.Seconds()))))
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusTooManyRequests)
	json.NewEncoder(w).Encode(map[string]string{"error": "too many requests"})
}

// Limit returns middleware that allows at most n requests per client IP
// within a rolling window of length per (see Limiter). Requests over the
// limit get WriteTooMany's 429.
func Limit(n int, per time.Duration) func(http.Handler) http.Handler {
	l := New(n, per)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if ok, retryAfter := l.Allow(ClientIP(r)); !ok {
				WriteTooMany(w, retryAfter)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// ClientIP returns the request's client IP: when TrustedProxyHops is above
// zero, the X-Forwarded-For entry TrustedProxyHops places from the right (the
// leftmost one when the header is shorter); otherwise the host portion of
// http.Request.RemoteAddr.
func ClientIP(r *http.Request) string {
	if TrustedProxyHops > 0 {
		// A client may send several X-Forwarded-For lines; proxies append to
		// the last, so read them all in order.
		if xff := strings.Join(r.Header.Values("X-Forwarded-For"), ","); xff != "" {
			entries := strings.Split(xff, ",")
			return strings.TrimSpace(entries[max(len(entries)-TrustedProxyHops, 0)])
		}
	}
	// RemoteAddr is host:port; the port is ephemeral per connection, so key
	// on the host only.
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}
