package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"

	"github.com/dogauzun/gelirgider/backend/internal/user"
)

// statusClientClosedRequest is nginx's non-standard status for a request
// its client cancelled; net/http has no constant for it.
const statusClientClosedRequest = 499

// WriteInternalError logs err at ERROR level with the request's method,
// path and user id, then writes the same generic 500 body handlers wrote
// before. Only the log gets the detail: repository errors are wrapped with
// the failing operation, so one line tells an operator which query failed
// in which handler instead of a bare "status":500 request line.
//
// It lives in auth, next to UserID, because that is where the user id in
// the request context comes from and every package serving a 500 already
// imports auth for it. It logs through slog.Default, which main sets to the
// server's JSON logger, so no logger has to be threaded through the handler
// constructors of six packages.
//
// One error is not internal: a user.MissingError for the signed-in user
// means an admin deleted that user after the session check, so it answers
// the 401 the user's next request gets, and logs nothing. Every
// lookup of the signed-in user by id, and every write whose user foreign key
// failed, lands here. A missing user other than the signed-in one is not
// matched: its route maps it to its own 404 first.
//
// Nor is any error on a request whose client went away: net/http cancels
// the request context when the connection closes, so the handler's query
// fails with context.Canceled although nothing is broken. It logs nothing
// and answers nginx's 499 "client closed request", which nobody receives
// but the request log records instead of a false 500.
func WriteInternalError(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(r.Context().Err(), context.Canceled) {
		WriteError(w, statusClientClosedRequest, "client closed request")
		return
	}
	userID, signedIn := UserID(r.Context())
	var missing user.MissingError
	if signedIn && errors.As(err, &missing) && missing.ID == userID {
		WriteError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	fields := []any{"method", r.Method, "path", r.URL.Path, "error", err.Error()}
	if signedIn {
		fields = append(fields, "user_id", userID)
	}
	slog.Error("internal error", fields...)
	WriteError(w, http.StatusInternalServerError, "internal error")
}

// WriteError answers status with the JSON body {"error": msg}.
func WriteError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

// WriteJSON answers status with v encoded as the JSON body. It encodes
// before writing the header: a value encoding/json refuses (+Inf, NaN)
// is a logged 500, not the chosen status with an empty body. The log
// names the payload type; the request log has the method, path and status.
func WriteJSON(w http.ResponseWriter, status int, v any) {
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(v); err != nil {
		slog.Error("encode response", "error", err, "type", fmt.Sprintf("%T", v))
		WriteError(w, http.StatusInternalServerError, "internal error")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	w.Write(buf.Bytes())
}

// DecodeJSON decodes the request body into v. On failure it answers 400
// "invalid request body" and returns false, so the handler just returns.
func DecodeJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		WriteError(w, http.StatusBadRequest, "invalid request body")
		return false
	}
	return true
}

// DecodeOptionalJSON is DecodeJSON for a route whose body may be absent: an
// empty body leaves v as it is and counts as success.
func DecodeOptionalJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(r.Body).Decode(v); err != nil && !errors.Is(err, io.EOF) {
		WriteError(w, http.StatusBadRequest, "invalid request body")
		return false
	}
	return true
}
