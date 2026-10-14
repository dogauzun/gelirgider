package httpbody

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestFetch(t *testing.T) {
	tests := []struct {
		name     string
		status   int
		body     string
		wantBody string
		wantCode int // StatusError code; 0 means no StatusError
		wantErr  bool
	}{
		{name: "ok", status: http.StatusOK, body: "hello", wantBody: "hello"},
		{name: "ok at the limit", status: http.StatusOK, body: "12345678", wantBody: "12345678"},
		{name: "not found", status: http.StatusNotFound, body: "gone", wantCode: http.StatusNotFound, wantErr: true},
		{name: "server error", status: http.StatusInternalServerError, wantCode: http.StatusInternalServerError, wantErr: true},
		{name: "over the limit", status: http.StatusOK, body: "123456789", wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer srv.Close()
			req, err := http.NewRequest(http.MethodGet, srv.URL, nil)
			if err != nil {
				t.Fatal(err)
			}

			body, err := Fetch(srv.Client(), req, 8)

			if (err != nil) != tc.wantErr {
				t.Fatalf("Fetch: err = %v, want error %v", err, tc.wantErr)
			}
			if string(body) != tc.wantBody {
				t.Errorf("Fetch: body = %q, want %q", body, tc.wantBody)
			}
			gotCode := 0
			if se := (*StatusError)(nil); errors.As(err, &se) {
				gotCode = se.Code
			}
			if gotCode != tc.wantCode {
				t.Errorf("Fetch: status code = %d, want %d (err %v)", gotCode, tc.wantCode, err)
			}
		})
	}
}

// TestFetchNilClientTimesOut checks that with a nil client, a server
// that never answers fails the fetch at the default client's timeout
// instead of holding it until the caller's context ends.
func TestFetchNilClientTimesOut(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(srv.Close)
	t.Cleanup(func() { close(release) })

	saved := defaultClient
	defaultClient = &http.Client{Timeout: 50 * time.Millisecond}
	t.Cleanup(func() { defaultClient = saved })

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	if _, err := Fetch(nil, req, 8); err == nil {
		t.Fatal("Fetch: got nil error, want timeout")
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("Fetch returned after %v, want the client timeout, not the context deadline", elapsed)
	}
}
