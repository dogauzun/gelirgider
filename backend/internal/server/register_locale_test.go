package server

import (
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"testing"
)

// TestRegisterLocaleNamesDefaultPortfolio covers the optional locale field of
// POST /api/auth/register: it names the seeded portfolio, a missing or empty
// value means tr, and anything but tr and en is rejected.
func TestRegisterLocaleNamesDefaultPortfolio(t *testing.T) {
	srv := newTestServer(t)

	bad := postJSON(t, http.DefaultClient, srv.URL+"/api/auth/register", map[string]string{
		"email": "dave@example.com", "password": "correct-horse", "locale": "de",
	})
	if bad.StatusCode != http.StatusBadRequest {
		t.Fatalf("register with locale=de status = %d, want %d", bad.StatusCode, http.StatusBadRequest)
	}
	var errBody map[string]string
	if err := json.NewDecoder(bad.Body).Decode(&errBody); err != nil {
		t.Fatalf("decode error body: %v", err)
	}
	if want := "locale: must be one of tr, en"; errBody["error"] != want {
		t.Fatalf("error = %q, want %q", errBody["error"], want)
	}

	for _, tc := range []struct {
		name string
		body map[string]string
		want string
	}{
		{"missing", map[string]string{}, "Portföy"},
		{"empty", map[string]string{"locale": ""}, "Portföy"},
		{"en", map[string]string{"locale": "en"}, "Portfolio"},
	} {
		jar, err := cookiejar.New(nil)
		if err != nil {
			t.Fatalf("cookiejar.New: %v", err)
		}
		client := &http.Client{Jar: jar}
		tc.body["email"] = tc.name + "@example.com"
		tc.body["password"] = "correct-horse"

		if resp := postJSON(t, client, srv.URL+"/api/auth/register", tc.body); resp.StatusCode != http.StatusCreated {
			t.Fatalf("%s: register status = %d, want %d", tc.name, resp.StatusCode, http.StatusCreated)
		}
		if resp := postJSON(t, client, srv.URL+"/api/auth/login", tc.body); resp.StatusCode != http.StatusOK {
			t.Fatalf("%s: login status = %d, want %d", tc.name, resp.StatusCode, http.StatusOK)
		}
		resp, err := client.Get(srv.URL + "/api/portfolios")
		if err != nil {
			t.Fatalf("%s: GET /api/portfolios: %v", tc.name, err)
		}
		var list []struct {
			Name string `json:"name"`
		}
		err = json.NewDecoder(resp.Body).Decode(&list)
		resp.Body.Close()
		if err != nil {
			t.Fatalf("%s: decode portfolios: %v", tc.name, err)
		}
		if len(list) != 1 || list[0].Name != tc.want {
			t.Fatalf("%s: portfolios = %+v, want one named %q", tc.name, list, tc.want)
		}
	}
}
