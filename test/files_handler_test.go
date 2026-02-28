package test

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"jotes/handlers"
)

// performJSONRequest sends one JSON request to a handler under test and returns
// the recorded response for later assertions.
//
// Parameters:
//   - t: the active test instance used for helper failure reporting.
//   - handler: the HTTP handler under test.
//   - method: the HTTP method to use for the request.
//   - target: the request URL path to send to the handler.
//   - payload: the Go value to encode as the request body, or nil for an empty body.
//
// Returns:
//   - *httptest.ResponseRecorder: the recorder containing the handler response.
func performJSONRequest(t *testing.T, handler http.HandlerFunc, method, target string, payload interface{}) *httptest.ResponseRecorder {
	t.Helper()

	var body bytes.Buffer
	if payload != nil {
		if err := json.NewEncoder(&body).Encode(payload); err != nil {
			t.Fatalf("encode request body: %v", err)
		}
	}

	request := httptest.NewRequest(method, target, &body)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

// performMultipartUploadRequest sends one multipart upload request with a
// single file and parent directory field to a handler under test.
//
// Parameters:
//   - t: the active test instance used for helper failure reporting.
//   - handler: the HTTP handler under test.
//   - target: the request URL path to send to the handler.
//   - parentPath: the managed parent directory path that should receive the uploaded file.
//   - fileName: the multipart filename that should be reported by the browser.
//   - fileContent: the exact file bytes that should be uploaded.
//
// Returns:
//   - *httptest.ResponseRecorder: the recorder containing the handler response.
func performMultipartUploadRequest(t *testing.T, handler http.HandlerFunc, target, parentPath, fileName string, fileContent []byte) *httptest.ResponseRecorder {
	t.Helper()

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("parent", parentPath); err != nil {
		t.Fatalf("write multipart parent field: %v", err)
	}

	fileWriter, err := writer.CreateFormFile("file", fileName)
	if err != nil {
		t.Fatalf("create multipart file field: %v", err)
	}
	if _, err := fileWriter.Write(fileContent); err != nil {
		t.Fatalf("write multipart file content: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}

	request := httptest.NewRequest(http.MethodPost, target, &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

// decodeJSONResponse parses one JSON HTTP response body into the supplied value
// and fails the test immediately when decoding is unsuccessful.
//
// Parameters:
//   - t: the active test instance used for helper failure reporting.
//   - response: the recorder whose body should be decoded.
//   - target: pointer to the destination value that should receive the JSON payload.
//
// Returns:
//   - none: target is populated directly when decoding succeeds.
func decodeJSONResponse(t *testing.T, response *httptest.ResponseRecorder, target interface{}) {
	t.Helper()

	if err := json.NewDecoder(response.Body).Decode(target); err != nil {
		t.Fatalf("decode response body: %v", err)
	}
}

// TestCreateFileHandlerDefaultsToMarkdownExtension verifies that creating a
// file without an explicit extension stores the note as Markdown.
//
// Parameters:
//   - t: the active Go test instance.
//
// Returns:
//   - none: the test fails when the handler does not append .md as expected.
func TestCreateFileHandlerDefaultsToMarkdownExtension(t *testing.T) {
	rootDir := t.TempDir()
	handler := handlers.CreateFileHandler(rootDir)
	response := performJSONRequest(t, handler, http.MethodPost, "/jotes/api/files/create", handlers.FileCreateRequest{
		Name:   "meeting-notes",
		Type:   "file",
		Parent: "/",
	})

	if response.Code != http.StatusCreated {
		t.Fatalf("expected status %d, got %d with body %s", http.StatusCreated, response.Code, response.Body.String())
	}

	var payload handlers.FileCreateResponse
	decodeJSONResponse(t, response, &payload)
	if payload.Path != "/meeting-notes.md" {
		t.Fatalf("expected created path /meeting-notes.md, got %q", payload.Path)
	}
	if _, err := os.Stat(filepath.Join(rootDir, "meeting-notes.md")); err != nil {
		t.Fatalf("expected created Markdown file to exist: %v", err)
	}
}

// TestCreateFileHandlerPreservesProvidedExtension verifies that creating a
// file with an explicit extension keeps that extension unchanged.
//
// Parameters:
//   - t: the active Go test instance.
//
// Returns:
//   - none: the test fails when the handler rewrites a supplied extension.
func TestCreateFileHandlerPreservesProvidedExtension(t *testing.T) {
	rootDir := t.TempDir()
	handler := handlers.CreateFileHandler(rootDir)
	response := performJSONRequest(t, handler, http.MethodPost, "/jotes/api/files/create", handlers.FileCreateRequest{
		Name:   "scratch.org",
		Type:   "file",
		Parent: "/",
	})

	if response.Code != http.StatusCreated {
		t.Fatalf("expected status %d, got %d with body %s", http.StatusCreated, response.Code, response.Body.String())
	}

	var payload handlers.FileCreateResponse
	decodeJSONResponse(t, response, &payload)
	if payload.Path != "/scratch.org" {
		t.Fatalf("expected created path /scratch.org, got %q", payload.Path)
	}
	if _, err := os.Stat(filepath.Join(rootDir, "scratch.org")); err != nil {
		t.Fatalf("expected created .org file to exist: %v", err)
	}
}

// TestCreateFileHandlerRejectsPathSeparators verifies that create requests are
// limited to one basename and cannot smuggle traversal separators.
//
// Parameters:
//   - t: the active Go test instance.
//
// Returns:
//   - none: the test fails when the handler accepts an unsafe basename.
func TestCreateFileHandlerRejectsPathSeparators(t *testing.T) {
	rootDir := t.TempDir()
	handler := handlers.CreateFileHandler(rootDir)
	response := performJSONRequest(t, handler, http.MethodPost, "/jotes/api/files/create", handlers.FileCreateRequest{
		Name:   "../escape.md",
		Type:   "file",
		Parent: "/",
	})

	if response.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d with body %s", http.StatusBadRequest, response.Code, response.Body.String())
	}
}

// TestCreateFileHandlerRejectsURLReservedCharacters verifies that create
// requests reject filenames whose raw characters would be interpreted as URL
// delimiters or escape prefixes by the browser and router.
//
// Parameters:
//   - t: the active Go test instance.
//
// Returns:
//   - none: the test fails when the handler accepts %, ?, or # in the basename.
func TestCreateFileHandlerRejectsURLReservedCharacters(t *testing.T) {
	rootDir := t.TempDir()
	handler := handlers.CreateFileHandler(rootDir)

	for _, invalidName := range []string{"topic?draft.md", "topic#draft.md", "a%2Fb.md"} {
		response := performJSONRequest(t, handler, http.MethodPost, "/jotes/api/files/create", handlers.FileCreateRequest{
			Name:   invalidName,
			Type:   "file",
			Parent: "/",
		})

		if response.Code != http.StatusBadRequest {
			t.Fatalf("expected status %d for %q, got %d with body %s", http.StatusBadRequest, invalidName, response.Code, response.Body.String())
		}
	}
}

// TestCreateFileHandlerReturnsCanonicalDirectoryPath verifies that directory
// creation responses use the same slashless directory path format as listings
// and move destinations.
//
// Parameters:
//   - t: the active Go test instance.
//
// Returns:
//   - none: the test fails when the handler returns a trailing slash path.
func TestCreateFileHandlerReturnsCanonicalDirectoryPath(t *testing.T) {
	rootDir := t.TempDir()
	handler := handlers.CreateFileHandler(rootDir)
	response := performJSONRequest(t, handler, http.MethodPost, "/jotes/api/files/create", handlers.FileCreateRequest{
		Name:   "projects",
		Type:   "directory",
		Parent: "/",
	})

	if response.Code != http.StatusCreated {
		t.Fatalf("expected status %d, got %d with body %s", http.StatusCreated, response.Code, response.Body.String())
	}

	var payload handlers.FileCreateResponse
	decodeJSONResponse(t, response, &payload)
	if payload.Path != "/projects" {
		t.Fatalf("expected created directory path /projects, got %q", payload.Path)
	}
	if info, err := os.Stat(filepath.Join(rootDir, "projects")); err != nil || !info.IsDir() {
		t.Fatalf("expected created directory to exist: %v", err)
	}
}

// TestCreateFileHandlerRejectsDanglingSymlinkDestination verifies that create
// requests refuse an existing dangling symlink instead of following it outside
// the configured root.
//
// Parameters:
//   - t: the active Go test instance.
//
// Returns:
//   - none: the test fails when the handler writes through the symlink target.
func TestCreateFileHandlerRejectsDanglingSymlinkDestination(t *testing.T) {
	rootDir := t.TempDir()
	outsideDir := t.TempDir()
	outsideTarget := filepath.Join(outsideDir, "escaped.md")
	if err := os.Symlink(outsideTarget, filepath.Join(rootDir, "escape.md")); err != nil {
		t.Fatalf("create dangling symlink: %v", err)
	}

	handler := handlers.CreateFileHandler(rootDir)
	response := performJSONRequest(t, handler, http.MethodPost, "/jotes/api/files/create", handlers.FileCreateRequest{
		Name:   "escape.md",
		Type:   "file",
		Parent: "/",
	})

	if response.Code != http.StatusConflict {
		t.Fatalf("expected status %d, got %d with body %s", http.StatusConflict, response.Code, response.Body.String())
	}
	if _, err := os.Stat(outsideTarget); !os.IsNotExist(err) {
		t.Fatalf("expected outside target to remain absent, stat error was %v", err)
	}
}

// TestCreateFileHandlerRejectsSymlinkedParent verifies that create requests do
// not allow a symlinked parent directory to act as an alias into another tree.
//
// Parameters:
//   - t: the active Go test instance.
//
// Returns:
//   - none: the test fails when the handler writes into the symlink target directory.
func TestCreateFileHandlerRejectsSymlinkedParent(t *testing.T) {
	rootDir := t.TempDir()
	outsideDir := t.TempDir()
	if err := os.Symlink(outsideDir, filepath.Join(rootDir, "linked-parent")); err != nil {
		t.Fatalf("create parent symlink: %v", err)
	}

	handler := handlers.CreateFileHandler(rootDir)
	response := performJSONRequest(t, handler, http.MethodPost, "/jotes/api/files/create", handlers.FileCreateRequest{
		Name:   "inside.md",
		Type:   "file",
		Parent: "/linked-parent",
	})

	if response.Code != http.StatusNotFound {
		t.Fatalf("expected status %d, got %d with body %s", http.StatusNotFound, response.Code, response.Body.String())
	}
	if _, err := os.Stat(filepath.Join(outsideDir, "inside.md")); !os.IsNotExist(err) {
		t.Fatalf("expected symlink target directory to remain unchanged, stat error was %v", err)
	}
}

// TestUploadFileHandlerStoresUploadedFile verifies that one multipart upload
// stores the file under its original browser-supplied basename and preserves
// the uploaded byte content exactly.
//
// Parameters:
//   - t: the active Go test instance.
//
// Returns:
//   - none: the test fails when the handler rejects or mutates the uploaded file.
func TestUploadFileHandlerStoresUploadedFile(t *testing.T) {
	rootDir := t.TempDir()
	handler := handlers.UploadFileHandler(rootDir)
	response := performMultipartUploadRequest(t, handler, "/jotes/api/files/upload", "/", "report.pdf", []byte("fake-pdf-data"))

	if response.Code != http.StatusCreated {
		t.Fatalf("expected status %d, got %d with body %s", http.StatusCreated, response.Code, response.Body.String())
	}

	var payload handlers.FileUploadResponse
	decodeJSONResponse(t, response, &payload)
	if payload.Path != "/report.pdf" {
		t.Fatalf("expected uploaded path /report.pdf, got %q", payload.Path)
	}
	if payload.BytesWritten != int64(len("fake-pdf-data")) {
		t.Fatalf("expected uploaded byte count %d, got %d", len("fake-pdf-data"), payload.BytesWritten)
	}

	uploadedBytes, err := os.ReadFile(filepath.Join(rootDir, "report.pdf"))
	if err != nil {
		t.Fatalf("read uploaded file: %v", err)
	}
	if string(uploadedBytes) != "fake-pdf-data" {
		t.Fatalf("expected uploaded content to round-trip exactly, got %q", string(uploadedBytes))
	}
}

// TestUploadFileHandlerRejectsURLReservedCharacters verifies that uploaded
// filenames still obey the managed basename validation rules used elsewhere in
// the file-management API.
//
// Parameters:
//   - t: the active Go test instance.
//
// Returns:
//   - none: the test fails when the upload handler accepts a URL-reserved filename.
func TestUploadFileHandlerRejectsURLReservedCharacters(t *testing.T) {
	rootDir := t.TempDir()
	handler := handlers.UploadFileHandler(rootDir)
	response := performMultipartUploadRequest(t, handler, "/jotes/api/files/upload", "/", "topic?draft.txt", []byte("invalid"))

	if response.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d with body %s", http.StatusBadRequest, response.Code, response.Body.String())
	}
}

// TestUploadFileHandlerRejectsSymlinkedParent verifies that uploaded files do
// not allow a symlinked parent directory to act as an alias into another tree.
//
// Parameters:
//   - t: the active Go test instance.
//
// Returns:
//   - none: the test fails when the upload handler writes into the symlink target directory.
func TestUploadFileHandlerRejectsSymlinkedParent(t *testing.T) {
	rootDir := t.TempDir()
	outsideDir := t.TempDir()
	if err := os.Symlink(outsideDir, filepath.Join(rootDir, "linked-parent")); err != nil {
		t.Fatalf("create parent symlink: %v", err)
	}

	handler := handlers.UploadFileHandler(rootDir)
	response := performMultipartUploadRequest(t, handler, "/jotes/api/files/upload", "/linked-parent", "inside.txt", []byte("blocked"))

	if response.Code != http.StatusNotFound {
		t.Fatalf("expected status %d, got %d with body %s", http.StatusNotFound, response.Code, response.Body.String())
	}
	if _, err := os.Stat(filepath.Join(outsideDir, "inside.txt")); !os.IsNotExist(err) {
		t.Fatalf("expected symlink target directory to remain unchanged, stat error was %v", err)
	}
}

// TestCreateFileHandlerRejectsManagedJotesParent verifies that create requests
// cannot place new files or directories directly inside one managed .jotes
// companion folder.
//
// Parameters:
//   - t: the active Go test instance.
//
// Returns:
//   - none: the test fails when the create API writes into a managed .jotes folder.
func TestCreateFileHandlerRejectsManagedJotesParent(t *testing.T) {
	rootDir := t.TempDir()
	if err := os.Mkdir(filepath.Join(rootDir, ".jotes-note.md"), 0o755); err != nil {
		t.Fatalf("create managed companion directory: %v", err)
	}

	handler := handlers.CreateFileHandler(rootDir)
	response := performJSONRequest(t, handler, http.MethodPost, "/jotes/api/files/create", handlers.FileCreateRequest{
		Name:   "inside.md",
		Type:   "file",
		Parent: "/.jotes-note.md",
	})
	if response.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d with body %s", http.StatusBadRequest, response.Code, response.Body.String())
	}
}

// TestUploadFileHandlerRejectsManagedJotesParent verifies that generic file
// uploads cannot place new files directly inside one managed .jotes companion
// folder.
//
// Parameters:
//   - t: the active Go test instance.
//
// Returns:
//   - none: the test fails when the upload API writes into a managed .jotes folder.
func TestUploadFileHandlerRejectsManagedJotesParent(t *testing.T) {
	rootDir := t.TempDir()
	if err := os.Mkdir(filepath.Join(rootDir, ".jotes-note.md"), 0o755); err != nil {
		t.Fatalf("create managed companion directory: %v", err)
	}

	handler := handlers.UploadFileHandler(rootDir)
	response := performMultipartUploadRequest(t, handler, "/jotes/api/files/upload", "/.jotes-note.md", "inside.txt", []byte("blocked"))
	if response.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d with body %s", http.StatusBadRequest, response.Code, response.Body.String())
	}
}

// TestDeleteFileHandlerRejectsManagedJotesPath verifies that delete requests
// cannot remove a managed .jotes companion folder directly through the generic
// file-management API.
//
// Parameters:
//   - t: the active Go test instance.
//
// Returns:
//   - none: the test fails when the delete API removes a managed companion folder.
func TestDeleteFileHandlerRejectsManagedJotesPath(t *testing.T) {
	rootDir := t.TempDir()
	if err := os.Mkdir(filepath.Join(rootDir, ".jotes-note.md"), 0o755); err != nil {
		t.Fatalf("create managed companion directory: %v", err)
	}

	handler := handlers.DeleteFileHandler(rootDir)
	response := performJSONRequest(t, handler, http.MethodPost, "/jotes/api/files/delete", handlers.FileDeleteRequest{
		Paths: []string{"/.jotes-note.md"},
	})
	if response.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d with body %s", http.StatusOK, response.Code, response.Body.String())
	}

	var payload handlers.FileDeleteResponse
	decodeJSONResponse(t, response, &payload)
	if payload.Success {
		t.Fatalf("expected delete request to fail, got %+v", payload)
	}
	if len(payload.Failed) == 0 || !strings.Contains(payload.Failed[0], "cannot be deleted directly") {
		t.Fatalf("expected managed companion delete failure, got %+v", payload.Failed)
	}
}

// TestDeleteFileHandlerRejectsSymlinkPath verifies that delete requests refuse
// to operate through a symlink alias path.
//
// Parameters:
//   - t: the active Go test instance.
//
// Returns:
//   - none: the test fails when the delete API removes the symlink target.
func TestDeleteFileHandlerRejectsSymlinkPath(t *testing.T) {
	rootDir := t.TempDir()
	outsideDir := t.TempDir()
	outsideTarget := filepath.Join(outsideDir, "note.md")
	if err := os.WriteFile(outsideTarget, []byte("outside"), 0o644); err != nil {
		t.Fatalf("create outside target file: %v", err)
	}
	if err := os.Symlink(outsideTarget, filepath.Join(rootDir, "alias.md")); err != nil {
		t.Fatalf("create symlink alias: %v", err)
	}

	handler := handlers.DeleteFileHandler(rootDir)
	response := performJSONRequest(t, handler, http.MethodPost, "/jotes/api/files/delete", handlers.FileDeleteRequest{
		Paths: []string{"/alias.md"},
	})

	if response.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d with body %s", http.StatusOK, response.Code, response.Body.String())
	}

	var payload handlers.FileDeleteResponse
	decodeJSONResponse(t, response, &payload)
	if payload.Success {
		t.Fatalf("expected symlink delete to fail, got %+v", payload)
	}
	if len(payload.Failed) == 0 || !strings.Contains(payload.Failed[0], "Cannot resolve path") {
		t.Fatalf("expected symlink delete failure, got %+v", payload.Failed)
	}
	if _, err := os.Stat(outsideTarget); err != nil {
		t.Fatalf("expected outside target file to remain present: %v", err)
	}
}

// TestMoveFileHandlerRejectsSymlinkSourcePath verifies that move requests do
// not operate through a symlink alias path.
//
// Parameters:
//   - t: the active Go test instance.
//
// Returns:
//   - none: the test fails when the move API renames the symlink target.
func TestMoveFileHandlerRejectsSymlinkSourcePath(t *testing.T) {
	rootDir := t.TempDir()
	outsideDir := t.TempDir()
	outsideTarget := filepath.Join(outsideDir, "note.md")
	if err := os.WriteFile(outsideTarget, []byte("outside"), 0o644); err != nil {
		t.Fatalf("create outside target file: %v", err)
	}
	if err := os.Symlink(outsideTarget, filepath.Join(rootDir, "alias.md")); err != nil {
		t.Fatalf("create symlink alias: %v", err)
	}
	if err := os.Mkdir(filepath.Join(rootDir, "archive"), 0o755); err != nil {
		t.Fatalf("create archive directory: %v", err)
	}

	handler := handlers.MoveFileHandler(rootDir)
	response := performJSONRequest(t, handler, http.MethodPost, "/jotes/api/files/move", handlers.FileMoveRequest{
		Sources:     []string{"/alias.md"},
		Destination: "/archive",
	})

	if response.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d with body %s", http.StatusOK, response.Code, response.Body.String())
	}

	var payload handlers.FileMoveResponse
	decodeJSONResponse(t, response, &payload)
	if payload.Success {
		t.Fatalf("expected symlink move to fail, got %+v", payload)
	}
	if len(payload.Failed) == 0 || !strings.Contains(payload.Failed[0], "Cannot resolve source") {
		t.Fatalf("expected symlink move failure, got %+v", payload.Failed)
	}
	if _, err := os.Stat(outsideTarget); err != nil {
		t.Fatalf("expected outside target file to remain present: %v", err)
	}
}

// TestMoveFileHandlerRejectsDanglingSymlinkCollision verifies that move
// requests treat a dangling symlink name in the destination directory as an
// existing entry rather than overwriting it.
//
// Parameters:
//   - t: the active Go test instance.
//
// Returns:
//   - none: the test fails when the move API reports success over the symlink collision.
func TestMoveFileHandlerRejectsDanglingSymlinkCollision(t *testing.T) {
	rootDir := t.TempDir()
	outsideDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(rootDir, "todo.md"), []byte("hello"), 0o644); err != nil {
		t.Fatalf("create source note: %v", err)
	}
	if err := os.Mkdir(filepath.Join(rootDir, "archive"), 0o755); err != nil {
		t.Fatalf("create archive directory: %v", err)
	}
	if err := os.Symlink(filepath.Join(outsideDir, "todo.md"), filepath.Join(rootDir, "archive", "todo.md")); err != nil {
		t.Fatalf("create dangling destination symlink: %v", err)
	}

	handler := handlers.MoveFileHandler(rootDir)
	response := performJSONRequest(t, handler, http.MethodPost, "/jotes/api/files/move", handlers.FileMoveRequest{
		Sources:     []string{"/todo.md"},
		Destination: "/archive",
	})

	if response.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d with body %s", http.StatusOK, response.Code, response.Body.String())
	}

	var payload handlers.FileMoveResponse
	decodeJSONResponse(t, response, &payload)
	if payload.Success {
		t.Fatalf("expected dangling symlink collision to fail, got %+v", payload)
	}
	if len(payload.Failed) == 0 || !strings.Contains(payload.Failed[0], "Destination already exists") {
		t.Fatalf("expected destination collision failure, got %+v", payload.Failed)
	}
	if _, err := os.Stat(filepath.Join(rootDir, "todo.md")); err != nil {
		t.Fatalf("expected source note to remain in place: %v", err)
	}
}

// TestMoveFileHandlerRejectsMovingDirectoryIntoDescendant verifies that the
// move API refuses to place a directory inside one of its own subdirectories.
//
// Parameters:
//   - t: the active Go test instance.
//
// Returns:
//   - none: the test fails when the unsafe move is allowed or the source disappears.
func TestMoveFileHandlerRejectsMovingDirectoryIntoDescendant(t *testing.T) {
	rootDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(rootDir, "projects", "subdir"), 0o755); err != nil {
		t.Fatalf("create test directories: %v", err)
	}

	handler := handlers.MoveFileHandler(rootDir)
	response := performJSONRequest(t, handler, http.MethodPost, "/jotes/api/files/move", handlers.FileMoveRequest{
		Sources:     []string{"/projects"},
		Destination: "/projects/subdir",
	})

	if response.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d with body %s", http.StatusOK, response.Code, response.Body.String())
	}

	var payload handlers.FileMoveResponse
	decodeJSONResponse(t, response, &payload)
	if payload.Success {
		t.Fatalf("expected move to fail, but success was reported: %+v", payload)
	}
	if len(payload.Failed) == 0 || !strings.Contains(payload.Failed[0], "subdirectories") {
		t.Fatalf("expected descendant-move failure message, got %+v", payload.Failed)
	}
	if _, err := os.Stat(filepath.Join(rootDir, "projects")); err != nil {
		t.Fatalf("expected source directory to remain in place: %v", err)
	}
}

// TestDeleteFileHandlerPrunesNestedSelections verifies that bulk deletion only
// processes the highest selected ancestor when nested paths are submitted in
// the same request.
//
// Parameters:
//   - t: the active Go test instance.
//
// Returns:
//   - none: the test fails when nested paths are reported separately or the ancestor survives.
func TestDeleteFileHandlerPrunesNestedSelections(t *testing.T) {
	rootDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(rootDir, "docs", "nested"), 0o755); err != nil {
		t.Fatalf("create nested directory tree: %v", err)
	}
	if err := os.WriteFile(filepath.Join(rootDir, "docs", "nested", "note.md"), []byte("hello"), 0o644); err != nil {
		t.Fatalf("create nested note: %v", err)
	}

	handler := handlers.DeleteFileHandler(rootDir)
	response := performJSONRequest(t, handler, http.MethodPost, "/jotes/api/files/delete", handlers.FileDeleteRequest{
		Paths: []string{"/docs", "/docs/nested/note.md"},
	})

	if response.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d with body %s", http.StatusOK, response.Code, response.Body.String())
	}

	var payload handlers.FileDeleteResponse
	decodeJSONResponse(t, response, &payload)
	if !payload.Success {
		t.Fatalf("expected delete to succeed, got %+v", payload)
	}
	if len(payload.Deleted) != 1 || payload.Deleted[0] != "/docs" {
		t.Fatalf("expected only /docs to be reported as deleted, got %+v", payload.Deleted)
	}
	if _, err := os.Stat(filepath.Join(rootDir, "docs")); !os.IsNotExist(err) {
		t.Fatalf("expected docs directory to be removed, stat error was %v", err)
	}
}

// TestMoveFileHandlerRejectsNoOpSameParent verifies that moving an entry into
// the directory it already lives in is rejected as a no-op.
//
// Parameters:
//   - t: the active Go test instance.
//
// Returns:
//   - none: the test fails when the move API reports success for a same-parent move.
func TestMoveFileHandlerRejectsNoOpSameParent(t *testing.T) {
	rootDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(rootDir, "todo.md"), []byte("hi"), 0o644); err != nil {
		t.Fatalf("create todo note: %v", err)
	}

	handler := handlers.MoveFileHandler(rootDir)
	response := performJSONRequest(t, handler, http.MethodPost, "/jotes/api/files/move", handlers.FileMoveRequest{
		Sources:     []string{"/todo.md"},
		Destination: "/",
	})

	if response.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d with body %s", http.StatusOK, response.Code, response.Body.String())
	}

	var payload handlers.FileMoveResponse
	decodeJSONResponse(t, response, &payload)
	if payload.Success {
		t.Fatalf("expected same-parent move to fail, got %+v", payload)
	}
	if len(payload.Failed) == 0 || !strings.Contains(payload.Failed[0], "already named") {
		t.Fatalf("expected no-op failure message, got %+v", payload.Failed)
	}
}

// TestMoveFileHandlerRenamesSingleEntryInPlace verifies that the move handler
// can rename one selected entry while keeping it in the same directory.
//
// Parameters:
//   - t: the active Go test instance.
//
// Returns:
//   - none: the test fails when the source is not renamed exactly once in place.
func TestMoveFileHandlerRenamesSingleEntryInPlace(t *testing.T) {
	rootDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(rootDir, "todo.md"), []byte("hi"), 0o644); err != nil {
		t.Fatalf("create todo note: %v", err)
	}

	handler := handlers.MoveFileHandler(rootDir)
	response := performJSONRequest(t, handler, http.MethodPost, "/jotes/api/files/move", handlers.FileMoveRequest{
		Sources:     []string{"/todo.md"},
		Destination: "/",
		TargetName:  "renamed.md",
	})

	if response.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d with body %s", http.StatusOK, response.Code, response.Body.String())
	}

	var payload handlers.FileMoveResponse
	decodeJSONResponse(t, response, &payload)
	if !payload.Success {
		t.Fatalf("expected rename to succeed, got %+v", payload)
	}
	if len(payload.Moved) != 1 || payload.Moved[0] != "/todo.md -> /renamed.md" {
		t.Fatalf("expected renamed path in response, got %+v", payload.Moved)
	}
	if _, err := os.Stat(filepath.Join(rootDir, "todo.md")); !os.IsNotExist(err) {
		t.Fatalf("expected original file to be gone, stat error was %v", err)
	}
	if _, err := os.Stat(filepath.Join(rootDir, "renamed.md")); err != nil {
		t.Fatalf("expected renamed file to exist: %v", err)
	}
}

// TestMoveFileHandlerRenamesManagedNoteCompanionDirectory verifies that
// renaming one Markdown note also renames its managed .jotes companion
// directory and rewrites note-relative pasted-image references in the note
// source.
//
// Parameters:
//   - t: the active Go test instance.
//
// Returns:
//   - none: the test fails when the note, companion directory, or note content are not updated together.
func TestMoveFileHandlerRenamesManagedNoteCompanionDirectory(t *testing.T) {
	rootDir := t.TempDir()
	originalContent := "![Pasted image](<.jotes-todo.md/pasted-image-20250101-010101.png>)\n"
	if err := os.WriteFile(filepath.Join(rootDir, "todo.md"), []byte(originalContent), 0o644); err != nil {
		t.Fatalf("create todo note: %v", err)
	}
	if err := os.Mkdir(filepath.Join(rootDir, ".jotes-todo.md"), 0o755); err != nil {
		t.Fatalf("create managed companion directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(rootDir, ".jotes-todo.md", "pasted-image-20250101-010101.png"), buildTinyPNGBytes(t), 0o644); err != nil {
		t.Fatalf("create managed companion image: %v", err)
	}

	handler := handlers.MoveFileHandler(rootDir)
	response := performJSONRequest(t, handler, http.MethodPost, "/jotes/api/files/move", handlers.FileMoveRequest{
		Sources:     []string{"/todo.md"},
		Destination: "/",
		TargetName:  "renamed.md",
	})
	if response.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d with body %s", http.StatusOK, response.Code, response.Body.String())
	}

	var payload handlers.FileMoveResponse
	decodeJSONResponse(t, response, &payload)
	if !payload.Success {
		t.Fatalf("expected rename to succeed, got %+v", payload)
	}
	if _, err := os.Stat(filepath.Join(rootDir, ".jotes-todo.md")); !os.IsNotExist(err) {
		t.Fatalf("expected original managed companion directory to be gone, stat error was %v", err)
	}
	if _, err := os.Stat(filepath.Join(rootDir, ".jotes-renamed.md")); err != nil {
		t.Fatalf("expected renamed managed companion directory to exist: %v", err)
	}

	renamedContentBytes, err := os.ReadFile(filepath.Join(rootDir, "renamed.md"))
	if err != nil {
		t.Fatalf("read renamed note: %v", err)
	}
	if strings.Contains(string(renamedContentBytes), ".jotes-todo.md/") {
		t.Fatalf("expected old managed companion prefix to be removed, got %q", string(renamedContentBytes))
	}
	if !strings.Contains(string(renamedContentBytes), ".jotes-renamed.md/") {
		t.Fatalf("expected new managed companion prefix, got %q", string(renamedContentBytes))
	}
}

// TestMoveFileHandlerMovesManagedNoteCompanionDirectory verifies that moving
// one Markdown note into another directory also moves its managed .jotes
// companion directory so pasted-image references remain sibling-relative.
//
// Parameters:
//   - t: the active Go test instance.
//
// Returns:
//   - none: the test fails when the note and companion directory do not move together.
func TestMoveFileHandlerMovesManagedNoteCompanionDirectory(t *testing.T) {
	rootDir := t.TempDir()
	if err := os.Mkdir(filepath.Join(rootDir, "archive"), 0o755); err != nil {
		t.Fatalf("create archive directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(rootDir, "todo.md"), []byte("![Pasted image](<.jotes-todo.md/image.png>)\n"), 0o644); err != nil {
		t.Fatalf("create todo note: %v", err)
	}
	if err := os.Mkdir(filepath.Join(rootDir, ".jotes-todo.md"), 0o755); err != nil {
		t.Fatalf("create managed companion directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(rootDir, ".jotes-todo.md", "image.png"), buildTinyPNGBytes(t), 0o644); err != nil {
		t.Fatalf("create managed companion image: %v", err)
	}

	handler := handlers.MoveFileHandler(rootDir)
	response := performJSONRequest(t, handler, http.MethodPost, "/jotes/api/files/move", handlers.FileMoveRequest{
		Sources:     []string{"/todo.md"},
		Destination: "/archive",
	})
	if response.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d with body %s", http.StatusOK, response.Code, response.Body.String())
	}

	var payload handlers.FileMoveResponse
	decodeJSONResponse(t, response, &payload)
	if !payload.Success {
		t.Fatalf("expected move to succeed, got %+v", payload)
	}
	if _, err := os.Stat(filepath.Join(rootDir, ".jotes-todo.md")); !os.IsNotExist(err) {
		t.Fatalf("expected original managed companion directory to be gone, stat error was %v", err)
	}
	if _, err := os.Stat(filepath.Join(rootDir, "archive", ".jotes-todo.md")); err != nil {
		t.Fatalf("expected moved managed companion directory to exist: %v", err)
	}

	movedContentBytes, err := os.ReadFile(filepath.Join(rootDir, "archive", "todo.md"))
	if err != nil {
		t.Fatalf("read moved note: %v", err)
	}
	if !strings.Contains(string(movedContentBytes), ".jotes-todo.md/") {
		t.Fatalf("expected sibling-relative companion prefix to remain unchanged, got %q", string(movedContentBytes))
	}
}

// TestMoveFileHandlerRejectsDirectManagedCompanionMove verifies that managed
// .jotes directories cannot be moved directly through the file-management API
// because they must remain attached to their owning note.
//
// Parameters:
//   - t: the active Go test instance.
//
// Returns:
//   - none: the test fails when the handler accepts a direct move of a managed companion directory.
func TestMoveFileHandlerRejectsDirectManagedCompanionMove(t *testing.T) {
	rootDir := t.TempDir()
	if err := os.Mkdir(filepath.Join(rootDir, ".jotes-todo.md"), 0o755); err != nil {
		t.Fatalf("create managed companion directory: %v", err)
	}
	if err := os.Mkdir(filepath.Join(rootDir, "archive"), 0o755); err != nil {
		t.Fatalf("create archive directory: %v", err)
	}

	handler := handlers.MoveFileHandler(rootDir)
	response := performJSONRequest(t, handler, http.MethodPost, "/jotes/api/files/move", handlers.FileMoveRequest{
		Sources:     []string{"/.jotes-todo.md"},
		Destination: "/archive",
	})
	if response.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d with body %s", http.StatusOK, response.Code, response.Body.String())
	}

	var payload handlers.FileMoveResponse
	decodeJSONResponse(t, response, &payload)
	if payload.Success {
		t.Fatalf("expected direct managed companion move to fail, got %+v", payload)
	}
	if len(payload.Failed) == 0 || !strings.Contains(payload.Failed[0], "cannot be moved directly") {
		t.Fatalf("expected managed companion failure message, got %+v", payload.Failed)
	}
}

// TestMoveFileHandlerRejectsManagedJotesDestination verifies that generic move
// requests cannot place normal files directly into one managed .jotes companion
// folder.
//
// Parameters:
//   - t: the active Go test instance.
//
// Returns:
//   - none: the test fails when the move API accepts a managed companion folder as the destination.
func TestMoveFileHandlerRejectsManagedJotesDestination(t *testing.T) {
	rootDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(rootDir, "todo.md"), []byte("hi"), 0o644); err != nil {
		t.Fatalf("create todo note: %v", err)
	}
	if err := os.Mkdir(filepath.Join(rootDir, ".jotes-todo.md"), 0o755); err != nil {
		t.Fatalf("create managed companion directory: %v", err)
	}

	handler := handlers.MoveFileHandler(rootDir)
	response := performJSONRequest(t, handler, http.MethodPost, "/jotes/api/files/move", handlers.FileMoveRequest{
		Sources:     []string{"/todo.md"},
		Destination: "/.jotes-todo.md",
	})
	if response.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d with body %s", http.StatusBadRequest, response.Code, response.Body.String())
	}
}

// TestMoveFileHandlerRejectsRenameForMultipleSources verifies that the rename
// path of the move API only accepts exactly one selected source.
//
// Parameters:
//   - t: the active Go test instance.
//
// Returns:
//   - none: the test fails when a multi-source rename request is accepted.
func TestMoveFileHandlerRejectsRenameForMultipleSources(t *testing.T) {
	rootDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(rootDir, "todo.md"), []byte("hi"), 0o644); err != nil {
		t.Fatalf("create todo note: %v", err)
	}
	if err := os.WriteFile(filepath.Join(rootDir, "draft.md"), []byte("bye"), 0o644); err != nil {
		t.Fatalf("create draft note: %v", err)
	}

	handler := handlers.MoveFileHandler(rootDir)
	response := performJSONRequest(t, handler, http.MethodPost, "/jotes/api/files/move", handlers.FileMoveRequest{
		Sources:     []string{"/todo.md", "/draft.md"},
		Destination: "/",
		TargetName:  "renamed.md",
	})

	if response.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d with body %s", http.StatusBadRequest, response.Code, response.Body.String())
	}
	if _, err := os.Stat(filepath.Join(rootDir, "todo.md")); err != nil {
		t.Fatalf("expected first source to remain present: %v", err)
	}
	if _, err := os.Stat(filepath.Join(rootDir, "draft.md")); err != nil {
		t.Fatalf("expected second source to remain present: %v", err)
	}
}

// TestListDirectoriesHandlerReturnsScopedMatches verifies that the move
// destination search only returns directories beneath the configured root and
// can filter them through the ripgrep-backed search path.
//
// Parameters:
//   - t: the active Go test instance.
//
// Returns:
//   - none: the test fails when the handler misses the expected match or leaks unrelated directories.
func TestListDirectoriesHandlerReturnsScopedMatches(t *testing.T) {
	if _, err := exec.LookPath("rg"); err != nil {
		t.Skip("ripgrep is required for directory search tests")
	}

	rootDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(rootDir, "notes", "archive"), 0o755); err != nil {
		t.Fatalf("create notes/archive directory: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(rootDir, "notes", "active"), 0o755); err != nil {
		t.Fatalf("create notes/active directory: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(rootDir, "assets"), 0o755); err != nil {
		t.Fatalf("create assets directory: %v", err)
	}

	handler := handlers.ListDirectoriesHandler(rootDir)
	response := performJSONRequest(t, handler, http.MethodGet, "/jotes/api/directories?q=arch&from=/notes", nil)

	if response.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d with body %s", http.StatusOK, response.Code, response.Body.String())
	}

	var payload handlers.DirectoryListResponse
	decodeJSONResponse(t, response, &payload)
	if len(payload.Directories) != 1 {
		t.Fatalf("expected exactly one matching directory, got %+v", payload.Directories)
	}
	if payload.Directories[0].Path != "/notes/archive" {
		t.Fatalf("expected /notes/archive, got %+v", payload.Directories[0])
	}
	if payload.Directories[0].Relative != "archive" {
		t.Fatalf("expected relative path archive, got %+v", payload.Directories[0])
	}
}
