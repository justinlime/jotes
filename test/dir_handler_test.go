package test

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"jotes/handlers"
	"jotes/models"
)

// capturingDirTemplate stores the most recent directory listing passed to the
// template interface so tests can inspect the handler's computed entry model.
type capturingDirTemplate struct {
	listing *models.DirListing
}

// ExecuteDir captures one rendered directory listing and reports success to the
// handler under test without executing a real HTML template.
//
// Parameters:
//   - w: the HTTP response writer provided by the handler under test.
//   - listing: the fully populated directory listing model that the handler wants to render.
//
// Returns:
//   - error: always nil so tests can inspect the captured listing directly.
func (t *capturingDirTemplate) ExecuteDir(w http.ResponseWriter, listing *models.DirListing) error {
	t.listing = listing
	return nil
}

// buildDirectoryListingForTest runs the directory handler for one target path
// and returns the captured listing model for later assertions.
//
// Parameters:
//   - t: the active Go test instance used for helper failure reporting.
//   - rootDir: the managed filesystem root that should back the handler.
//   - targetPath: the request path that should be rendered as a directory listing.
//
// Returns:
//   - *models.DirListing: the captured listing model produced by the handler.
func buildDirectoryListingForTest(t *testing.T, rootDir, targetPath string) *models.DirListing {
	t.Helper()

	templateCapture := &capturingDirTemplate{}
	handler := handlers.DirHandler(rootDir, "Jotes", "catppuccin", templateCapture)
	request := httptest.NewRequest(http.MethodGet, targetPath, nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d with body %s", http.StatusOK, response.Code, response.Body.String())
	}
	if templateCapture.listing == nil {
		t.Fatalf("expected captured directory listing for %s", targetPath)
	}

	return templateCapture.listing
}

// findDirectoryEntryByName locates one entry by basename inside a captured
// directory listing.
//
// Parameters:
//   - t: the active Go test instance used for helper failure reporting.
//   - listing: the captured directory listing whose entries should be searched.
//   - entryName: the basename that should be found inside listing.Entries.
//
// Returns:
//   - models.FileEntry: the first entry whose Name matches entryName exactly.
func findDirectoryEntryByName(t *testing.T, listing *models.DirListing, entryName string) models.FileEntry {
	t.Helper()

	for _, entry := range listing.Entries {
		if entry.Name == entryName {
			return entry
		}
	}

	t.Fatalf("expected to find directory entry %q in %+v", entryName, listing.Entries)
	return models.FileEntry{}
}

// TestDirHandlerClassifiesJotesDirectoriesSeparatelyFromHiddenFiles verifies
// that managed .jotes directories are tracked by the dedicated companion-folder
// visibility mask instead of the generic hidden-files toggle.
//
// Parameters:
//   - t: the active Go test instance.
//
// Returns:
//   - none: the test fails when the captured listing misclassifies the .jotes row.
func TestDirHandlerClassifiesJotesDirectoriesSeparatelyFromHiddenFiles(t *testing.T) {
	rootDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(rootDir, "note.md"), []byte("# Note\n"), 0o644); err != nil {
		t.Fatalf("create note: %v", err)
	}
	if err := os.Mkdir(filepath.Join(rootDir, ".jotes-note.md"), 0o755); err != nil {
		t.Fatalf("create companion directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(rootDir, ".jotes-note.md", "image.png"), buildTinyPNGBytes(t), 0o644); err != nil {
		t.Fatalf("create companion image: %v", err)
	}

	listing := buildDirectoryListingForTest(t, rootDir, "/")
	companionEntry := findDirectoryEntryByName(t, listing, ".jotes-note.md")
	if !companionEntry.IsJotesCompanion {
		t.Fatalf("expected .jotes directory to be marked as managed companion content, got %+v", companionEntry)
	}
	if companionEntry.IsHidden {
		t.Fatalf("expected .jotes directory to avoid the generic hidden-files toggle, got %+v", companionEntry)
	}
	if companionEntry.VisibilityMask != 240 {
		t.Fatalf("expected .jotes visibility mask 240, got %+v", companionEntry)
	}
}

// TestDirHandlerMarksFilesInsideJotesDirectoryAsNonNotes verifies that text
// files stored inside managed .jotes folders do not masquerade as user notes in
// the directory model.
//
// Parameters:
//   - t: the active Go test instance.
//
// Returns:
//   - none: the test fails when the captured listing classifies managed companion content as a note.
func TestDirHandlerMarksFilesInsideJotesDirectoryAsNonNotes(t *testing.T) {
	rootDir := t.TempDir()
	if err := os.Mkdir(filepath.Join(rootDir, ".jotes-note.md"), 0o755); err != nil {
		t.Fatalf("create companion directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(rootDir, ".jotes-note.md", "nested.md"), []byte("# Managed content\n"), 0o644); err != nil {
		t.Fatalf("create nested markdown file: %v", err)
	}

	listing := buildDirectoryListingForTest(t, rootDir, "/.jotes-note.md")
	nestedEntry := findDirectoryEntryByName(t, listing, "nested.md")
	if !nestedEntry.IsJotesCompanion {
		t.Fatalf("expected nested .jotes file to remain managed companion content, got %+v", nestedEntry)
	}
	if nestedEntry.IsNote {
		t.Fatalf("expected nested .jotes markdown file to be treated as auxiliary content, got %+v", nestedEntry)
	}
	if nestedEntry.VisibilityMask != 240 {
		t.Fatalf("expected nested .jotes file visibility mask 240, got %+v", nestedEntry)
	}
}

// TestListDirectoriesHandlerExcludesJotesDirectories verifies that move
// destination search results never include managed .jotes directories.
//
// Parameters:
//   - t: the active Go test instance.
//
// Returns:
//   - none: the test fails when the directory search API leaks a managed .jotes path.
func TestListDirectoriesHandlerExcludesJotesDirectories(t *testing.T) {
	rootDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(rootDir, "notes", "archive"), 0o755); err != nil {
		t.Fatalf("create notes/archive directory: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(rootDir, ".jotes-note.md"), 0o755); err != nil {
		t.Fatalf("create companion directory: %v", err)
	}

	handler := handlers.ListDirectoriesHandler(rootDir)
	response := performJSONRequest(t, handler, http.MethodGet, "/jotes/api/directories", nil)
	if response.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d with body %s", http.StatusOK, response.Code, response.Body.String())
	}

	var payload handlers.DirectoryListResponse
	decodeJSONResponse(t, response, &payload)
	for _, directory := range payload.Directories {
		if strings.Contains(directory.Path, ".jotes-") {
			t.Fatalf("expected managed .jotes directory to be excluded, got %+v", payload.Directories)
		}
	}
}
