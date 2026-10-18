package auth

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/dogauzun/gelirgider/backend/internal/user"
)

// RegisterAdminRoutes wires the user management endpoints into mux. The
// caller must wrap mux with RequireUser and RequireAdmin. onRegister is the
// same seeding hook RegisterRoutes takes, so an admin-created user gets the
// same default data as a self-registered one. These endpoints only ever
// expose account fields, never another user's portfolios or transactions.
func RegisterAdminRoutes(mux *http.ServeMux, users *user.Repo, sessions *Repo, onRegister func(userID int64, locale string) error) {
	h := &handler{users: users, sessions: sessions, onRegister: onRegister}
	mux.HandleFunc("GET /api/admin/users", h.listUsers)
	mux.HandleFunc("POST /api/admin/users", h.adminCreateUser)
	mux.HandleFunc("POST /api/admin/users/{id}/password", h.adminSetPassword)
	mux.HandleFunc("PUT /api/admin/users/{id}/admin", h.adminSetAdmin)
	mux.HandleFunc("DELETE /api/admin/users/{id}", h.adminDeleteUser)
}

type adminUserResponse struct {
	ID        int64  `json:"id"`
	Email     string `json:"email"`
	IsAdmin   bool   `json:"is_admin"`
	CreatedAt string `json:"created_at"`
}

func toAdminUser(u user.User) adminUserResponse {
	return adminUserResponse{ID: u.ID, Email: u.Email, IsAdmin: u.IsAdmin, CreatedAt: u.CreatedAt.UTC().Format(time.RFC3339)}
}

// listUsers handles GET /api/admin/users.
//
//	@Summary	List users
//	@Tags		admin
//	@Produce	json
//	@Success	200	{array}		auth.adminUserResponse
//	@Failure	500	{object}	object{error=string}	"internal error"
//	@Router		/admin/users [get]
func (h *handler) listUsers(w http.ResponseWriter, r *http.Request) {
	users, err := h.users.List(r.Context())
	if err != nil {
		WriteInternalError(w, r, err)
		return
	}
	out := make([]adminUserResponse, 0, len(users))
	for _, u := range users {
		out = append(out, toAdminUser(u))
	}
	WriteJSON(w, http.StatusOK, out)
}

// adminCreateUser works whether registration is open or closed: it is the
// way an admin adds users when it is closed. It applies register's
// validation, duplicate-email error and onRegister seeding.
//
//	@Summary	Create a user
//	@Description	Works whether registration is open or closed. Seeds the default portfolio like register.
//	@Tags		admin
//	@Accept		json
//	@Produce	json
//	@Param		body	body		auth.credentials	true	"email, password and locale (tr or en, default tr)"
//	@Success	201	{object}	auth.adminUserResponse
//	@Failure	400	{object}	object{error=string}	"invalid request body; invalid email; email must be at most 254 characters; password must not be only whitespace; password must be at least 8 characters; password must be at most 72 bytes; locale: must be one of tr, en"
//	@Failure	404	{object}	object{error=string}	"user not found"
//	@Failure	409	{object}	object{error=string}	"email already registered"
//	@Failure	500	{object}	object{error=string}	"internal error"
//	@Router		/admin/users [post]
func (h *handler) adminCreateUser(w http.ResponseWriter, r *http.Request) {
	var creds credentials
	if !DecodeJSON(w, r, &creds) {
		return
	}
	email, locale, msg := validateRegistration(creds)
	if msg != "" {
		WriteError(w, http.StatusBadRequest, msg)
		return
	}
	u, err := h.createUser(r.Context(), email, creds.Password, locale)
	if errors.Is(err, user.ErrEmailTaken) {
		WriteError(w, http.StatusConflict, "email already registered")
		return
	}
	// Another admin deleted the new user before its default portfolio was
	// seeded: the 404 the other admin routes give for it.
	if errors.Is(err, user.ErrNotFound) {
		WriteError(w, http.StatusNotFound, "user not found")
		return
	}
	if err != nil {
		WriteInternalError(w, r, err)
		return
	}
	WriteJSON(w, http.StatusCreated, toAdminUser(u))
}

// pathUser loads the user named by the {id} path value, writing 404 for an
// unknown or malformed id.
func (h *handler) pathUser(w http.ResponseWriter, r *http.Request) (user.User, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		WriteError(w, http.StatusNotFound, "user not found")
		return user.User{}, false
	}
	u, err := h.users.GetByID(r.Context(), id)
	if errors.Is(err, user.ErrNotFound) {
		WriteError(w, http.StatusNotFound, "user not found")
		return user.User{}, false
	}
	if err != nil {
		WriteInternalError(w, r, err)
		return user.User{}, false
	}
	return u, true
}

// adminSetPassword replaces a user's password, signs out every session of
// that user (the admin's own too when resetting their own) and turns their
// two-step login off.
//
//	@Summary	Set a user's password
//	@Description	Signs out every session of that user and turns their two-step login off, so they log in with the new password alone and can enrol again from the account page.
//	@Tags		admin
//	@Accept		json
//	@Produce	json
//	@Param		id		path	int	true	"user id"
//	@Param		body	body		object{password=string}	true	"new password"
//	@Success	204
//	@Failure	400	{object}	object{error=string}	"invalid request body; password must not be only whitespace; password must be at least 8 characters; password must be at most 72 bytes"
//	@Failure	404	{object}	object{error=string}	"user not found"
//	@Failure	500	{object}	object{error=string}	"internal error"
//	@Router		/admin/users/{id}/password [post]
func (h *handler) adminSetPassword(w http.ResponseWriter, r *http.Request) {
	u, ok := h.pathUser(w, r)
	if !ok {
		return
	}
	var req struct {
		Password string `json:"password"`
	}
	if !DecodeJSON(w, r, &req) {
		return
	}
	// SetPassword validates too, but its error does not tell a bad password
	// from a DB failure, so check here to answer 400.
	if msg := validatePassword(req.Password); msg != "" {
		WriteError(w, http.StatusBadRequest, msg)
		return
	}
	err := SetPassword(r.Context(), h.users, h.sessions, u.Email, req.Password)
	// Another admin deleted the user since pathUser.
	if errors.Is(err, user.ErrNotFound) {
		WriteError(w, http.StatusNotFound, "user not found")
		return
	}
	if err != nil {
		WriteInternalError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// adminSetAdmin promotes or demotes a user. Demoting the last admin is
// refused with 409; demoting yourself is fine while another admin exists.
//
//	@Summary	Promote or demote a user
//	@Tags		admin
//	@Accept		json
//	@Produce	json
//	@Param		id		path	int	true	"user id"
//	@Param		body	body		object{is_admin=bool}	true	"is_admin is required"
//	@Success	204
//	@Failure	400	{object}	object{error=string}	"invalid request body"
//	@Failure	404	{object}	object{error=string}	"user not found"
//	@Failure	409	{object}	object{error=string}	"last admin"
//	@Failure	500	{object}	object{error=string}	"internal error"
//	@Router		/admin/users/{id}/admin [put]
func (h *handler) adminSetAdmin(w http.ResponseWriter, r *http.Request) {
	u, ok := h.pathUser(w, r)
	if !ok {
		return
	}
	var req struct {
		IsAdmin *bool `json:"is_admin"`
	}
	if !DecodeJSON(w, r, &req) {
		return
	}
	if req.IsAdmin == nil {
		WriteError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	var err error
	if *req.IsAdmin {
		err = h.users.SetAdmin(r.Context(), u.ID, true)
	} else {
		err = h.users.Demote(r.Context(), u.ID)
	}
	writeAdminMutation(w, r, err)
}

// adminDeleteUser deletes a user with all of their data (user.Repo.Delete).
// An admin cannot delete themselves, and the last admin cannot be deleted.
//
//	@Summary	Delete a user
//	@Description	Deletes the user with all of their data.
//	@Tags		admin
//	@Produce	json
//	@Param		id		path	int	true	"user id"
//	@Success	204
//	@Failure	404	{object}	object{error=string}	"user not found"
//	@Failure	409	{object}	object{error=string}	"cannot delete yourself; last admin"
//	@Failure	500	{object}	object{error=string}	"internal error"
//	@Router		/admin/users/{id} [delete]
func (h *handler) adminDeleteUser(w http.ResponseWriter, r *http.Request) {
	u, ok := h.pathUser(w, r)
	if !ok {
		return
	}
	if u.ID == MustUserID(r.Context()) {
		WriteError(w, http.StatusConflict, "cannot delete yourself")
		return
	}
	writeAdminMutation(w, r, h.users.Delete(r.Context(), u.ID))
}

// writeAdminMutation writes the response of an admin change to a user:
// 409 for the last admin, 404 for a missing user, 204 on success.
func writeAdminMutation(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, user.ErrLastAdmin):
		WriteError(w, http.StatusConflict, "last admin")
	case errors.Is(err, user.ErrNotFound):
		WriteError(w, http.StatusNotFound, "user not found")
	case err != nil:
		WriteInternalError(w, r, err)
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}
