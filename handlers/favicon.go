package handlers

import (
	"bytes"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// FaviconHandler serves the application's favicon from either a custom file or
// the embedded default asset.
//
// Parameters:
//   - embeddedFS: the embedded static filesystem that contains images/favicon.svg.
//   - faviconPath: an optional filesystem path to a custom favicon file; when empty the embedded default is used.
//
// Returns:
//   - http.HandlerFunc: a handler that serves the chosen favicon with an appropriate content type.
func FaviconHandler(embeddedFS fs.FS, faviconPath string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if faviconPath != "" {
			data, err := os.ReadFile(faviconPath)
			if err != nil {
				http.Error(w, "favicon not found", http.StatusNotFound)
				return
			}
			info, err := os.Stat(faviconPath)
			if err != nil {
				http.Error(w, "favicon not found", http.StatusNotFound)
				return
			}
			w.Header().Set("Content-Type", mimeForExtension(faviconPath))
			w.Header().Set("Cache-Control", "no-cache")
			http.ServeContent(w, r, "favicon", info.ModTime(), bytes.NewReader(data))
			return
		}

		// Default: serve the embedded favicon.svg.
		data, err := fs.ReadFile(embeddedFS, "images/favicon.svg")
		if err != nil {
			http.Error(w, "favicon not found", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "image/svg+xml")
		w.Header().Set("Cache-Control", "no-cache")
		http.ServeContent(w, r, "favicon.svg", time.Time{}, bytes.NewReader(data))
	}
}

// mimeForExtension returns the favicon response MIME type implied by a file's extension.
//
// Parameters:
//   - path: the favicon file path or file name whose extension should be inspected.
//
// Returns:
//   - string: the HTTP Content-Type value that should be used when serving the favicon.
func mimeForExtension(path string) string {
	switch filepath.Ext(path) {
	case ".svg":
		return "image/svg+xml"
	case ".png":
		return "image/png"
	case ".webp":
		return "image/webp"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	default:
		return "image/x-icon"
	}
}
