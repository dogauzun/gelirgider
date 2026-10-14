package auth

import (
	"context"
	"errors"
	"net/http"

	"github.com/dogauzun/gelirgider/backend/internal/user"
)

type contextKey int

const userIDKey contextKey = 0

// RequireUser wraps next, rejecting requests that lack a valid session
// cookie with 401 and otherwise making the authenticated user id available
// to next (and to outer middleware such as request logging) via UserID.
func (r *Repo) RequireUser(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		c, err := req.Cookie(cookieName)
		if err != nil {
			WriteError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		userID, err := r.GetSession(req.Context(), c.Value)
		if err != nil {
			WriteError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		// Mutate the request in place rather than passing a derived copy
		// downstream, so middleware wrapping this one (e.g. request
		// logging) can also read the user id via UserID(req.Context()).
		*req = *req.WithContext(context.WithValue(req.Context(), userIDKey, userID))
		next.ServeHTTP(w, req)
	})
}

// RequireAdmin wraps next, which must already sit behind RequireUser, and
// rejects requests whose session user is not an admin with 403. The flag is
// read from the DB on every request, so a demotion applies at once.
func RequireAdmin(users *user.Repo, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		u, err := users.GetByID(req.Context(), MustUserID(req.Context()))
		if err != nil && !errors.Is(err, user.ErrNotFound) {
			WriteInternalError(w, req, err)
			return
		}
		if !u.IsAdmin {
			WriteError(w, http.StatusForbidden, "admin only")
			return
		}
		next.ServeHTTP(w, req)
	})
}

// UserID returns the authenticated user id stored in ctx by RequireUser.
// This and MustUserID are the only sanctioned ways handlers and
// repositories read the current user id; UserID is for code that also runs
// without a session, such as request logging.
func UserID(ctx context.Context) (int64, bool) {
	id, ok := ctx.Value(userIDKey).(int64)
	return id, ok
}

// MustUserID is UserID for code behind RequireUser, where the id is always
// set. A missing id is a wiring bug, not a client error: it panics, and the
// server's outermost withRecover answers 500.
func MustUserID(ctx context.Context) int64 {
	id, ok := UserID(ctx)
	if !ok {
		panic("auth: no user id in request context, route is not behind RequireUser")
	}
	return id
}
