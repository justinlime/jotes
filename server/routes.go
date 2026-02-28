package server

import (
	"io/fs"
	"log"
	"net/http"

	"jotes/auth"
	"jotes/handlers"
)

// securityHeaders wraps h and adds defensive response headers to every reply
// that the main Jotes application server sends.
//
// Parameters:
//   - h: the fully configured application handler tree.
//
// Returns:
//   - http.Handler: a middleware-wrapped handler that sets CSP, nosniff,
//     clickjacking, and referrer controls before delegating to h.
func securityHeaders(h http.Handler) http.Handler {
	csp := "default-src 'self'; " +
		"script-src 'self' 'unsafe-inline'; " +
		"style-src 'self' 'unsafe-inline'; " +
		"img-src 'self' data: https:; " +
		"font-src 'self'; " +
		"frame-src 'self'; " +
		"object-src 'none';"

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", csp)
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "SAMEORIGIN")
		w.Header().Set("Referrer-Policy", "same-origin")
		h.ServeHTTP(w, r)
	})
}

// registerRoutes attaches every HTTP route that the Jotes UI needs.
//
// Parameters:
//   - mux: the ServeMux that receives all route registrations.
//   - authStore: the SQLite-backed auth store used by login, setup, and admin routes.
//   - rootDir: the single filesystem directory that backs the UI.
//   - theme: the Chroma highlight theme used for generated CSS and previews.
//   - siteName: the branding label shown in templates and page titles.
//   - defaultTheme: the default dark/light class applied to the root HTML element.
//   - tmpl: the compiled template set used to render pages.
//
// Returns:
//   - none: the supplied mux is mutated in place.
func registerRoutes(mux *http.ServeMux, authStore *auth.Store, rootDir, theme, siteName, defaultTheme string, tmpl *Templates) {
	mux.Handle("/static/", http.StripPrefix("/static/", staticHandler()))

	staticSub, err := fs.Sub(staticFS, "static")
	if err != nil {
		log.Fatalf("static sub fs for favicon: %v", err)
	}
	mux.HandleFunc("/favicon.ico", handlers.FaviconHandler(staticSub, ""))

	mux.HandleFunc("/jotes/login", handlers.LoginHandler(authStore, siteName, defaultTheme, tmpl))
	mux.HandleFunc("/jotes/setup", handlers.SetupHandler(authStore, siteName, defaultTheme, tmpl))
	mux.HandleFunc("/jotes/logout", handlers.LogoutHandler(authStore))
	mux.HandleFunc("/jotes/admin", handlers.AdminUsersHandler(authStore, siteName, defaultTheme, tmpl))
	mux.HandleFunc("/jotes/admin/users/new", handlers.AdminCreateUserHandler(authStore, siteName, defaultTheme, tmpl))
	mux.Handle("/jotes/admin/users/", handlers.AdminUserDetailHandler(authStore, siteName, defaultTheme, tmpl))

	mux.HandleFunc("/api/search", handlers.SearchHandler(rootDir))
	mux.Handle("/view/", http.StripPrefix("/view", handlers.ViewHandler(rootDir)))
	mux.HandleFunc("/highlight.css", handlers.HighlightCSSHandler(theme))
	mux.HandleFunc("/jotes/api/render", handlers.RenderEditorHandler(rootDir))
	mux.HandleFunc("/jotes/api/save", handlers.SaveEditorHandler(rootDir))
	mux.HandleFunc("/jotes/api/editor/upload-image", handlers.UploadEditorImageHandler(rootDir))
	mux.HandleFunc("/jotes/api/files/create", handlers.CreateFileHandler(rootDir))
	mux.HandleFunc("/jotes/api/files/upload", handlers.UploadFileHandler(rootDir))
	mux.HandleFunc("/jotes/api/files/delete", handlers.DeleteFileHandler(rootDir))
	mux.HandleFunc("/jotes/api/files/move", handlers.MoveFileHandler(rootDir))
	mux.HandleFunc("/jotes/api/directories", handlers.ListDirectoriesHandler(rootDir))
	mux.Handle("/jotes/edit/", http.StripPrefix("/jotes/edit", handlers.EditHandler(rootDir, siteName, defaultTheme, tmpl)))
	mux.HandleFunc("/", handlers.UniversalHandler(rootDir, siteName, defaultTheme, tmpl))
}
