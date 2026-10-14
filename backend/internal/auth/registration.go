package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/dogauzun/gelirgider/backend/internal/settings"
)

// registrationOpenKey holds the registration toggle (ADR 0010): "true" or
// "false". A missing row means closed.
const registrationOpenKey = "auth.registration_open"

func registrationOpen(ctx context.Context, s *settings.Repo) (bool, error) {
	v, ok, err := s.Get(ctx, registrationOpenKey)
	if err != nil {
		return false, err
	}
	return ok && v == "true", nil
}

// authConfig handles GET /api/auth/config, public: what the login and
// register pages need to know before anyone is logged in.
//
//	@Summary	Auth config
//	@Description	Needs no session.
//	@Tags		auth
//	@Produce	json
//	@Success	200	{object}	object{registration_open=bool}
//	@Failure	500	{object}	object{error=string}	"internal error"
//	@Router		/auth/config [get]
func (h *handler) authConfig(w http.ResponseWriter, r *http.Request) {
	open, err := registrationOpen(r.Context(), h.settings)
	if err != nil {
		WriteInternalError(w, r, err)
		return
	}
	WriteJSON(w, http.StatusOK, map[string]bool{"registration_open": open})
}

// SetRegistrationHandler handles PUT /api/admin/registration, body
// {"open": bool}. The caller mounts it behind RequireAdmin.
//
//	@Summary	Open or close registration
//	@Tags		admin
//	@Accept		json
//	@Produce	json
//	@Param		body	body		object{open=bool}	true	"open is required"
//	@Success	204
//	@Failure	400	{object}	object{error=string}	"invalid request body"
//	@Failure	500	{object}	object{error=string}	"internal error"
//	@Router		/admin/registration [put]
func SetRegistrationHandler(s *settings.Repo) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Open *bool `json:"open"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Open == nil {
			WriteError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		if err := s.Set(r.Context(), registrationOpenKey, strconv.FormatBool(*req.Open)); err != nil {
			WriteInternalError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
}
