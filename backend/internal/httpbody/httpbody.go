// Package httpbody holds the one fetch step every market-data provider
// shares: send a request, require 200 and read a size-capped body, plus the
// default client used when a provider has none. URLs, headers, 404 handling
// and parsing stay in each provider (ADR 0003: no provider interface).
package httpbody

import (
	"fmt"
	"io"
	"net/http"
	"time"
)

// defaultClient is used when a provider's HTTPClient is nil. Its timeout
// bounds one request, so a server that never answers fails the fetch
// instead of holding the caller until its context ends (GLG-396).
var defaultClient = &http.Client{Timeout: 30 * time.Second}

// Client returns c, or the default client when c is nil.
func Client(c *http.Client) *http.Client {
	if c == nil {
		return defaultClient
	}
	return c
}

// StatusError is a response other than 200. Code lets a caller treat one
// status, such as 404, as "no data" instead of a failure.
type StatusError struct {
	Code   int
	Status string
}

func (e *StatusError) Error() string { return "unexpected status " + e.Status }

// Fetch sends req with client (the default client when nil) and returns
// the body of a 200 response. A non-200 status is a *StatusError; a body
// over limit bytes is an error, never a silently truncated body.
func Fetch(client *http.Client, req *http.Request, limit int) ([]byte, error) {
	resp, err := Client(client).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, &StatusError{Code: resp.StatusCode, Status: resp.Status}
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, int64(limit)+1))
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}
	if len(body) > limit {
		return nil, fmt.Errorf("read body: over %d bytes", limit)
	}
	return body, nil
}
