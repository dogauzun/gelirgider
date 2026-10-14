package server

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/dogauzun/gelirgider/backend/internal/auth"
	"github.com/dogauzun/gelirgider/backend/internal/evds"
	"github.com/dogauzun/gelirgider/backend/internal/ratelimit"
	"github.com/dogauzun/gelirgider/backend/internal/settings"
	"github.com/dogauzun/gelirgider/backend/internal/us"
)

// providerKeys maps each data provider to the secret setting holding its
// API key (ADR 0010), in the order GET lists them.
var providerKeys = []struct{ name, setting string }{
	{"finnhub", us.FinnhubKeySetting},
	{"tiingo", us.TiingoKeySetting},
	{"evds", evds.KeySetting},
}

// providerSetting returns the secret setting for provider name, or "" for
// an unknown provider.
func providerSetting(name string) string {
	for _, p := range providerKeys {
		if p.name == name {
			return p.setting
		}
	}
	return ""
}

type providerStatus struct {
	Name       string `json:"name"`
	Configured bool   `json:"configured"`
	Last4      string `json:"last4"`
	// Error is "undecryptable" when the stored key cannot be read (SECRET_KEY
	// changed), so the admin can enter it again.
	Error string `json:"error,omitempty"`
}

// registerProviderRoutes adds the admin provider-key endpoints to mux, which
// must sit behind RequireAdmin. The API never returns a key, only whether
// it is set and its last 4 characters. fetcher runs the Finnhub and Tiingo
// key tests, evdsFetcher the EVDS one.
func registerProviderRoutes(mux *http.ServeMux, secrets *settings.Repo, fetcher *us.Fetcher, evdsFetcher *evds.Fetcher) {
	// Whether each provider has a key and its last 4 characters, never the
	// key itself.
	//
	//	@Summary	List data providers
	//	@Description	Each provider's key status: whether it is set and its last 4 characters, never the key. error is "undecryptable" when the stored key cannot be read.
	//	@Tags		admin
	//	@Produce	json
	//	@Success	200	{array}		server.providerStatus
	//	@Failure	500	{object}	object{error=string}	"internal error"
	//	@Router		/admin/providers [get]
	mux.HandleFunc("GET /api/admin/providers", func(w http.ResponseWriter, r *http.Request) {
		out := make([]providerStatus, 0, len(providerKeys))
		for _, p := range providerKeys {
			key, ok, err := secrets.GetSecret(r.Context(), p.setting)
			if errors.Is(err, settings.ErrUndecryptable) {
				out = append(out, providerStatus{Name: p.name, Configured: true, Error: "undecryptable"})
				continue
			}
			if err != nil {
				auth.WriteInternalError(w, r, err)
				return
			}
			s := providerStatus{Name: p.name, Configured: ok}
			if len(key) > 4 {
				s.Last4 = key[len(key)-4:]
			}
			out = append(out, s)
		}
		auth.WriteJSON(w, http.StatusOK, out)
	})

	// Stores a provider's API key; an empty key clears it.
	//
	//	@Summary	Set a provider API key
	//	@Tags		admin
	//	@Accept		json
	//	@Produce	json
	//	@Param		name	path	string					true	"provider name"	Enums(finnhub, tiingo, evds)
	//	@Param		body	body	object{api_key=string}	true	"API key; empty clears it"
	//	@Success	204
	//	@Failure	400	{object}	object{error=string}	"invalid JSON body"
	//	@Failure	404	{object}	object{error=string}	"unknown provider"
	//	@Failure	500	{object}	object{error=string}	"internal error"
	//	@Router		/admin/providers/{name} [put]
	mux.HandleFunc("PUT /api/admin/providers/{name}", func(w http.ResponseWriter, r *http.Request) {
		setting := providerSetting(r.PathValue("name"))
		if setting == "" {
			auth.WriteError(w, http.StatusNotFound, "unknown provider")
			return
		}
		var body struct {
			APIKey string `json:"api_key"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			auth.WriteError(w, http.StatusBadRequest, "invalid JSON body")
			return
		}
		// An empty key clears the setting.
		if err := secrets.SetSecret(r.Context(), setting, strings.TrimSpace(body.APIKey)); err != nil {
			auth.WriteInternalError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})

	// POST .../test makes one request to the provider with the body's
	// api_key, or the stored key when the body has none. Rate
	// limited like login, so it cannot be used to hammer the providers.
	//
	//	@Summary	Test a provider API key
	//	@Description	Makes one request to the provider with the body's api_key, or the stored key when the body has none. ok is false with the provider's reason in error when the key does not work. Rate limited to 10 requests per minute.
	//	@Tags		admin
	//	@Accept		json
	//	@Produce	json
	//	@Param		name	path	string					true	"provider name"	Enums(finnhub, tiingo, evds)
	//	@Param		body	body	object{api_key=string}	false	"key to test; empty tests the stored key"
	//	@Success	200	{object}	object{ok=bool,error=string}
	//	@Failure	400	{object}	object{error=string}	"invalid JSON body; stored key unreadable; not configured"
	//	@Failure	404	{object}	object{error=string}	"unknown provider"
	//	@Failure	500	{object}	object{error=string}	"internal error"
	//	@Router		/admin/providers/{name}/test [post]
	mux.Handle("POST /api/admin/providers/{name}/test", ratelimit.Limit(10, time.Minute)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		setting := providerSetting(r.PathValue("name"))
		if setting == "" {
			auth.WriteError(w, http.StatusNotFound, "unknown provider")
			return
		}
		var body struct {
			APIKey string `json:"api_key"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil && !errors.Is(err, io.EOF) {
			auth.WriteError(w, http.StatusBadRequest, "invalid JSON body")
			return
		}
		key := strings.TrimSpace(body.APIKey)
		if key == "" {
			var err error
			key, _, err = secrets.GetSecret(r.Context(), setting)
			if errors.Is(err, settings.ErrUndecryptable) {
				auth.WriteError(w, http.StatusBadRequest, "stored key unreadable")
				return
			}
			if err != nil {
				auth.WriteInternalError(w, r, err)
				return
			}
		}
		if key == "" {
			auth.WriteError(w, http.StatusBadRequest, "not configured")
			return
		}
		var reason string
		if setting == evds.KeySetting {
			reason = evdsFetcher.TestKey(r.Context(), key)
		} else {
			reason = fetcher.TestKey(r.Context(), setting, key)
		}
		auth.WriteJSON(w, http.StatusOK, struct {
			OK    bool   `json:"ok"`
			Error string `json:"error,omitempty"`
		}{reason == "", reason})
	})))
}
