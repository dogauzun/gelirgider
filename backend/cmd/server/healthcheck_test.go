package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestIsHealthy(t *testing.T) {
	ok := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer ok.Close()

	down := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer down.Close()

	if !isHealthy(portOf(ok.URL)) {
		t.Error("expected healthy for 200 response")
	}
	if isHealthy(portOf(down.URL)) {
		t.Error("expected unhealthy for 500 response")
	}
	if isHealthy("0") {
		t.Error("expected unhealthy when nothing is listening")
	}
}

// portOf extracts the port from an httptest server URL (http://127.0.0.1:PORT),
// since isHealthy always dials localhost:port.
func portOf(url string) string {
	return strings.TrimPrefix(url, "http://127.0.0.1:")
}
