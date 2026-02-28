package auth

import (
	"net/http"
	"time"
)

// SetSessionCookie writes the persistent Jotes session cookie after a
// successful login or first-run setup.
//
// Parameters:
//   - w: the HTTP response writer that should receive the Set-Cookie header.
//   - rawToken: the opaque session token returned by the auth store.
//   - lifetime: the maximum age and expiry window that the cookie should advertise.
//   - secureOnly: true when the cookie should be marked Secure for HTTPS requests.
//
// Returns:
//   - none: the cookie header is written directly to w.
func SetSessionCookie(w http.ResponseWriter, rawToken string, lifetime time.Duration, secureOnly bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookieName,
		Value:    rawToken,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   secureOnly,
		MaxAge:   int(lifetime.Seconds()),
	})
}

// ClearSessionCookie expires the browser's Jotes session cookie during logout
// or when middleware discovers an invalid session.
//
// Parameters:
//   - w: the HTTP response writer that should receive the clearing Set-Cookie header.
//   - secureOnly: true when the cookie should be marked Secure for HTTPS requests.
//
// Returns:
//   - none: the cookie header is written directly to w.
func ClearSessionCookie(w http.ResponseWriter, secureOnly bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   secureOnly,
		MaxAge:   -1,
	})
}
