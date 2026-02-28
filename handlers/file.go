package handlers

import (
	"net/http"
	"os"
	"path"
	"path/filepath"
)

// ViewHandler serves a file inline without forcing attachment mode.
// Image previews use this route so the browser can render the file directly.
//
// Parameters:
//   - rootDir: the single filesystem directory exposed by the server.
//
// Returns:
//   - http.HandlerFunc: a handler that streams the requested file or returns 404 for directories and missing paths.
func ViewHandler(rootDir string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		urlPath := path.Clean("/" + r.URL.Path)

		fsPath, err := resolvePath(rootDir, urlPath)
		if err != nil {
			http.Error(w, "Not found", http.StatusNotFound)
			return
		}

		info, err := os.Stat(fsPath)
		if err != nil || info.IsDir() {
			http.Error(w, "Not found", http.StatusNotFound)
			return
		}

		file, err := os.Open(fsPath)
		if err != nil {
			http.Error(w, "Could not open file", http.StatusInternalServerError)
			return
		}
		defer file.Close()

		w.Header().Set("Content-Type", mimeForName(fsPath))
		http.ServeContent(w, r, filepath.Base(fsPath), info.ModTime(), file)
	}
}
