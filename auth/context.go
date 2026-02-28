package auth

import (
	"context"
	"net/http"
)

type currentUserContextKey struct{}

// WithCurrentUser returns a shallow request copy whose context carries the
// authenticated user identity resolved by auth middleware.
//
// Parameters:
//   - r: the incoming HTTP request that should gain auth context.
//   - user: the authenticated user identity to attach; nil clears any existing value.
//
// Returns:
//   - *http.Request: a request copy whose context stores user for downstream handlers.
func WithCurrentUser(r *http.Request, user *CurrentUser) *http.Request {
	ctx := context.WithValue(r.Context(), currentUserContextKey{}, user)
	return r.WithContext(ctx)
}

// CurrentUserFromRequest extracts the authenticated user identity previously
// attached to a request by auth middleware.
//
// Parameters:
//   - r: the HTTP request whose context may carry an authenticated user.
//
// Returns:
//   - *CurrentUser: the authenticated user identity when present, otherwise nil.
func CurrentUserFromRequest(r *http.Request) *CurrentUser {
	user, _ := r.Context().Value(currentUserContextKey{}).(*CurrentUser)
	return user
}
