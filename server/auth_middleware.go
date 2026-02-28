package server

import (
	"net/http"
	"net/url"
	"strings"

	"jotes/auth"
)

// authMiddleware enforces Jotes authentication for every note-related route,
// redirecting anonymous users to login and redirecting first-run installs to
// the initial admin setup flow before any notes can be viewed.
//
// Parameters:
//   - store: the SQLite-backed auth store used to inspect accounts and sessions.
//   - next: the fully configured application handler tree that should run after auth succeeds.
//
// Returns:
//   - http.Handler: a middleware-wrapped handler that attaches the authenticated user to each authorized request.
func authMiddleware(store *auth.Store, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestPath := r.URL.Path
		if isPublicAssetPath(requestPath) {
			next.ServeHTTP(w, r)
			return
		}

		currentUser, hadInvalidCookie, err := sessionUserFromRequest(store, r)
		if err != nil {
			http.Error(w, "Authentication is unavailable", http.StatusInternalServerError)
			return
		}
		if hadInvalidCookie {
			auth.ClearSessionCookie(w, r.TLS != nil)
		}
		if currentUser != nil {
			r = auth.WithCurrentUser(r, currentUser)
		}

		hasUsers, err := store.HasUsers()
		if err != nil {
			http.Error(w, "Authentication is unavailable", http.StatusInternalServerError)
			return
		}

		if !hasUsers {
			if requestPath == "/jotes/setup" {
				w.Header().Set("Cache-Control", "no-store")
				next.ServeHTTP(w, r)
				return
			}
			http.Redirect(w, r, "/jotes/setup?notice=login_required", http.StatusSeeOther)
			return
		}

		if requestPath == "/jotes/setup" {
			http.Redirect(w, r, "/jotes/login", http.StatusSeeOther)
			return
		}
		if requestPath == "/jotes/login" {
			w.Header().Set("Cache-Control", "no-store")
			next.ServeHTTP(w, r)
			return
		}

		if currentUser == nil {
			http.Redirect(w, r, "/jotes/login?next="+url.QueryEscape(r.URL.RequestURI()), http.StatusSeeOther)
			return
		}

		w.Header().Set("Cache-Control", "private, no-store")
		next.ServeHTTP(w, r)
	})
}

// isPublicAssetPath reports whether one request path should bypass auth checks
// because it serves shared static assets or public login/setup pages.
//
// Parameters:
//   - requestPath: the URL path being evaluated.
//
// Returns:
//   - bool: true when requestPath is a public asset or first-run/login route, otherwise false.
func isPublicAssetPath(requestPath string) bool {
	switch {
	case strings.HasPrefix(requestPath, "/static/"):
		return true
	case requestPath == "/favicon.ico":
		return true
	case requestPath == "/highlight.css":
		return true
	case requestPath == "/jotes/login":
		return true
	case requestPath == "/jotes/setup":
		return true
	default:
		return false
	}
}

// sessionUserFromRequest resolves the current request's session cookie to an
// authenticated user identity and reports whether the cookie existed but no
// longer mapped to a valid session.
//
// Parameters:
//   - store: the SQLite-backed auth store used to resolve the cookie token.
//   - r: the current HTTP request whose cookies should be inspected.
//
// Returns:
//   - *auth.CurrentUser: the authenticated user when the session token is valid, otherwise nil.
//   - bool: true when a session cookie was present but invalid or expired and should be cleared in the response.
//   - error: non-nil when session lookup fails unexpectedly.
func sessionUserFromRequest(store *auth.Store, r *http.Request) (*auth.CurrentUser, bool, error) {
	cookie, err := r.Cookie(auth.SessionCookieName)
	if err != nil {
		if err == http.ErrNoCookie {
			return nil, false, nil
		}
		return nil, false, err
	}

	user, err := store.LookupSession(cookie.Value)
	if err != nil {
		return nil, false, err
	}
	if user == nil {
		return nil, true, nil
	}

	return user, false, nil
}
