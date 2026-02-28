package server

import (
	"embed"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"time"

	"jotes/auth"
	"jotes/config"
	"jotes/handlers"
)

const (
	appName        = "Jotes"
	defaultTheme   = "dark"
	highlightTheme = "catppuccin-mocha"
)

// staticFS stores the embedded static asset filesystem provided by main.
var staticFS embed.FS

// SetStaticFS injects the embedded static asset filesystem that staticHandler
// later serves under the /static/ URL prefix.
//
// Parameters:
//   - efs: the embedded filesystem containing the repository's static/ subtree.
//
// Returns:
//   - none: the package-level static filesystem reference is updated in place.
func SetStaticFS(efs embed.FS) {
	staticFS = efs
}

// staticHandler creates an HTTP handler that serves files from the embedded
// static/ subtree.
//
// Parameters:
//   - none: the handler reads from the package-level staticFS set by SetStaticFS.
//
// Returns:
//   - http.Handler: a file server rooted at the embedded static/ directory.
func staticHandler() http.Handler {
	sub, err := fs.Sub(staticFS, "static")
	if err != nil {
		log.Fatalf("static sub fs: %v", err)
	}
	return http.FileServer(http.FS(sub))
}

// Run configures and starts the Jotes HTTP server for note browsing, preview,
// editing, and management routes.
//
// Parameters:
//   - cfg: validated runtime configuration including host, port, root directory, and auth data directory.
//   - templateFS: the embedded filesystem that contains the HTML templates.
//
// Returns:
//   - error: the result of http.Server.ListenAndServe, or any startup error encountered before serving begins.
func Run(cfg *config.Config, templateFS embed.FS) error {
	tmpl, err := LoadTemplates(templateFS)
	if err != nil {
		return fmt.Errorf("loading templates: %w", err)
	}

	authStore, err := auth.OpenStore(cfg.DataDir)
	if err != nil {
		return fmt.Errorf("opening auth store: %w", err)
	}
	defer authStore.Close()

	mux := http.NewServeMux()
	registerRoutes(mux, authStore, cfg.RootDir, highlightTheme, appName, defaultTheme, tmpl)
	wrappedMux := securityHeaders(authMiddleware(authStore, mux))

	// Configure the document renderer before any preview request is served.
	handlers.InitRenderOptions(highlightTheme)

	addr := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)
	logStartup(cfg, authStore.DatabasePath(), addr)

	// Warm the root directory listing so large top-level pages can reuse cached entries.
	handlers.WarmDirectoryListingCache(cfg.RootDir)

	// Watch the note tree so cached directory listings reflect file additions, removals, renames, and metadata updates.
	if err := handlers.StartWatcher(cfg.RootDir); err != nil {
		log.Printf("watcher: could not start filesystem watcher: %v", err)
	}

	srv := &http.Server{
		Addr:    addr,
		Handler: wrappedMux,

		// ReadHeaderTimeout limits how long a client may spend sending request
		// headers, protecting the server against Slowloris-style connections.
		ReadHeaderTimeout: 20 * time.Second,

		// IdleTimeout closes keep-alive connections that sit idle for too long,
		// reclaiming goroutines and file descriptors from abandoned clients.
		IdleTimeout: 120 * time.Second,
	}

	return srv.ListenAndServe()
}

// logStartup prints a concise summary of the active Jotes configuration.
//
// Parameters:
//   - cfg: the validated runtime configuration being used for this process.
//   - databasePath: the full filesystem path of the SQLite auth database file.
//   - addr: the fully formatted listen address passed to http.Server.
//
// Returns:
//   - none: the summary is written to the standard logger for operator visibility.
func logStartup(cfg *config.Config, databasePath, addr string) {
	sep := "-------------------------------------------"
	log.Println(sep)
	log.Printf("  %s", appName)
	log.Println(sep)
	log.Printf("  %-18s %s", "Address:", "http://"+addr)
	log.Printf("  %-18s %s", "Host:", cfg.Host)
	log.Printf("  %-18s %d", "Port:", cfg.Port)
	log.Printf("  %-18s %s", "Root directory:", cfg.RootDir)
	log.Printf("  %-18s %s", "Data directory:", cfg.DataDir)
	log.Printf("  %-18s %s", "Database:", databasePath)
	log.Println(sep)
}
