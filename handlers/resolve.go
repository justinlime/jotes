package handlers

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// pathWithinRoot reports whether candidate is equal to rootDir or nested
// beneath it after both paths are cleaned.
//
// Parameters:
//   - rootDir: the trusted filesystem root that bounds all note access.
//   - candidate: the filesystem path being checked against rootDir.
//
// Returns:
//   - bool: true when candidate stays inside rootDir, otherwise false.
func pathWithinRoot(rootDir, candidate string) bool {
	cleanRoot := filepath.Clean(rootDir)
	cleanCandidate := filepath.Clean(candidate)
	return cleanCandidate == cleanRoot || strings.HasPrefix(cleanCandidate, cleanRoot+string(filepath.Separator))
}

// resolvePath converts a URL path inside the Jotes UI into an absolute
// filesystem path under the single configured root directory.
//
// Parameters:
//   - rootDir: the absolute root directory that Jotes exposes as "/".
//   - urlPath: a cleaned URL path beginning with "/", such as "/notes/todo.md" or "/".
//
// Returns:
//   - string: the absolute filesystem path that corresponds to urlPath.
//   - error: non-nil when the path is malformed, escapes rootDir lexically, or resolves through a symlink outside rootDir.
func resolvePath(rootDir, urlPath string) (string, error) {
	if !strings.HasPrefix(urlPath, "/") {
		return "", fmt.Errorf("invalid path %q: URL paths must start with /", urlPath)
	}

	cleanRoot := filepath.Clean(rootDir)
	resolvedRoot, err := filepath.EvalSymlinks(cleanRoot)
	if err != nil {
		return "", fmt.Errorf("resolve configured root %q: %w", rootDir, err)
	}

	relPath := filepath.FromSlash(strings.TrimPrefix(urlPath, "/"))
	cleanPath := filepath.Clean(filepath.Join(resolvedRoot, relPath))
	if !pathWithinRoot(resolvedRoot, cleanPath) {
		return "", fmt.Errorf("path traversal detected for %q", urlPath)
	}

	resolvedPath, err := filepath.EvalSymlinks(cleanPath)
	if err == nil {
		if !pathWithinRoot(resolvedRoot, resolvedPath) {
			return "", fmt.Errorf("path %q resolves outside the configured root", urlPath)
		}
		return resolvedPath, nil
	}
	if !os.IsNotExist(err) {
		return "", fmt.Errorf("resolve symlinks for %q: %w", urlPath, err)
	}

	return cleanPath, nil
}
