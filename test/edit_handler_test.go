package test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"jotes/handlers"
	"jotes/models"
)

// buildTinyPNGBytes renders one in-memory 1x1 PNG image so upload tests can
// send a real image payload through the pasted-image handler.
//
// Parameters:
//   - t: the active Go test instance used for helper failure reporting.
//
// Returns:
//   - []byte: the encoded PNG file bytes for one opaque 1x1 pixel image.
func buildTinyPNGBytes(t *testing.T) []byte {
	t.Helper()

	var body bytes.Buffer
	img := image.NewNRGBA(image.Rect(0, 0, 1, 1))
	img.Set(0, 0, color.NRGBA{R: 137, G: 180, B: 250, A: 255})
	if err := png.Encode(&body, img); err != nil {
		t.Fatalf("encode tiny png: %v", err)
	}

	return body.Bytes()
}

// performEditorImageUploadRequest sends one multipart pasted-image upload
// request to the editor image handler under test.
//
// Parameters:
//   - t: the active Go test instance used for helper failure reporting.
//   - handler: the HTTP handler under test.
//   - target: the request URL path to send to the handler.
//   - notePath: the managed note path that should own the uploaded pasted image.
//   - fileName: the multipart filename that should be reported by the browser.
//   - fileContent: the exact uploaded file bytes.
//
// Returns:
//   - *httptest.ResponseRecorder: the recorder containing the handler response.
func performEditorImageUploadRequest(t *testing.T, handler http.HandlerFunc, target, notePath, fileName string, fileContent []byte) *httptest.ResponseRecorder {
	t.Helper()

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("path", notePath); err != nil {
		t.Fatalf("write note path field: %v", err)
	}

	fileWriter, err := writer.CreateFormFile("file", fileName)
	if err != nil {
		t.Fatalf("create image file field: %v", err)
	}
	if _, err := fileWriter.Write(fileContent); err != nil {
		t.Fatalf("write uploaded image bytes: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}

	request := httptest.NewRequest(http.MethodPost, target, &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	request.Header.Set("X-Jotes-Editor", "1")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

// performEditorImageUploadRequestWithDeclaredType sends one multipart pasted-image
// upload request whose file part declares an explicit Content-Type so tests can
// verify the server does not trust spoofed browser metadata.
//
// Parameters:
//   - t: the active Go test instance used for helper failure reporting.
//   - handler: the HTTP handler under test.
//   - target: the request URL path to send to the handler.
//   - notePath: the managed note path that should own the uploaded pasted image.
//   - fileName: the multipart filename that should be reported by the browser.
//   - declaredContentType: the explicit multipart Content-Type header to apply to the uploaded file part.
//   - fileContent: the exact uploaded file bytes.
//
// Returns:
//   - *httptest.ResponseRecorder: the recorder containing the handler response.
func performEditorImageUploadRequestWithDeclaredType(t *testing.T, handler http.HandlerFunc, target, notePath, fileName, declaredContentType string, fileContent []byte) *httptest.ResponseRecorder {
	t.Helper()

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("path", notePath); err != nil {
		t.Fatalf("write note path field: %v", err)
	}

	partHeader := textproto.MIMEHeader{}
	partHeader.Set("Content-Disposition", fmt.Sprintf(`form-data; name="file"; filename="%s"`, fileName))
	partHeader.Set("Content-Type", declaredContentType)
	partWriter, err := writer.CreatePart(partHeader)
	if err != nil {
		t.Fatalf("create typed image part: %v", err)
	}
	if _, err := partWriter.Write(fileContent); err != nil {
		t.Fatalf("write typed uploaded bytes: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}

	request := httptest.NewRequest(http.MethodPost, target, &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	request.Header.Set("X-Jotes-Editor", "1")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

// decodeEditorImageUploadResponse parses one editor image-upload response body
// into the supplied target structure and fails the test when decoding fails.
//
// Parameters:
//   - t: the active Go test instance used for helper failure reporting.
//   - response: the recorder whose response body should be decoded.
//   - target: pointer to the destination value that should receive the decoded JSON payload.
//
// Returns:
//   - none: target is populated directly when decoding succeeds.
func decodeEditorImageUploadResponse(t *testing.T, response *httptest.ResponseRecorder, target interface{}) {
	t.Helper()

	if err := json.NewDecoder(response.Body).Decode(target); err != nil {
		t.Fatalf("decode editor image upload response: %v", err)
	}
}

// buildEditorRevisionToken recreates the editor revision digest format used by
// the production save handler so tests can submit one valid save payload.
//
// Parameters:
//   - content: the exact note text whose revision token should be generated.
//
// Returns:
//   - string: the hexadecimal SHA-256 digest used by save requests.
func buildEditorRevisionToken(content string) string {
	digest := sha256.Sum256([]byte(content))
	return hex.EncodeToString(digest[:])
}

// performEditorSaveRequest sends one JSON save request to the editor save
// handler under test using caller-supplied headers that mimic browser-originated
// save requests.
//
// Parameters:
//   - t: the active Go test instance used for helper failure reporting.
//   - handler: the HTTP handler under test.
//   - target: the request URL path to send to the handler.
//   - payload: the JSON-serializable request body that should be posted.
//   - headers: optional HTTP headers that should be applied to the request before dispatch.
//
// Returns:
//   - *httptest.ResponseRecorder: the recorder containing the handler response.
func performEditorSaveRequest(t *testing.T, handler http.HandlerFunc, target string, payload interface{}, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()

	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal editor save request: %v", err)
	}

	request := httptest.NewRequest(http.MethodPost, target, bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Jotes-Editor", "1")
	for name, value := range headers {
		if strings.EqualFold(name, "Host") {
			request.Host = value
			continue
		}
		request.Header.Set(name, value)
	}

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

// noopEditorTemplate satisfies the editor template interface for tests that
// only need the handler's status code or side effects.
type noopEditorTemplate struct{}

// ExecuteEditor satisfies the editor template interface without rendering any
// output so tests can exercise EditHandler error paths directly.
//
// Parameters:
//   - w: the HTTP response writer provided by the handler under test.
//   - data: the editor page model that would be rendered during a successful request.
//
// Returns:
//   - error: always nil because the helper intentionally performs no rendering.
func (t *noopEditorTemplate) ExecuteEditor(w http.ResponseWriter, data *models.EditorData) error {
	return nil
}

// capturingPreviewTemplate stores the most recent preview model passed through
// the preview template interface so tests can inspect the computed CanEdit
// state for managed .jotes paths.
type capturingPreviewTemplate struct {
	preview *models.PreviewData
}

// ExecuteDir satisfies the shared template interface for tests that only care
// about preview rendering and therefore do not inspect directory listings.
//
// Parameters:
//   - w: the HTTP response writer provided by the handler under test.
//   - listing: the directory listing model that would be rendered for directory requests.
//
// Returns:
//   - error: always nil because preview tests ignore directory rendering.
func (t *capturingPreviewTemplate) ExecuteDir(w http.ResponseWriter, listing *models.DirListing) error {
	return nil
}

// ExecutePreview captures one preview model and reports success to the handler
// under test without executing a real HTML template.
//
// Parameters:
//   - w: the HTTP response writer provided by the handler under test.
//   - preview: the fully populated preview model that the handler wants to render.
//
// Returns:
//   - error: always nil so tests can inspect the captured preview directly.
func (t *capturingPreviewTemplate) ExecutePreview(w http.ResponseWriter, preview *models.PreviewData) error {
	t.preview = preview
	return nil
}

// TestUploadEditorImageHandlerStoresMarkdownPastedImage verifies that one
// pasted image uploaded for a Markdown note is stored beneath the managed
// sibling .jotes directory and returns the expected Markdown-relative path.
//
// Parameters:
//   - t: the active Go test instance.
//
// Returns:
//   - none: the test fails when the handler does not create the expected managed asset path.
func TestUploadEditorImageHandlerStoresMarkdownPastedImage(t *testing.T) {
	rootDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(rootDir, "test.md"), []byte("# Title\n"), 0o644); err != nil {
		t.Fatalf("create markdown note: %v", err)
	}

	handler := handlers.UploadEditorImageHandler(rootDir)
	response := performEditorImageUploadRequest(t, handler, "/jotes/api/editor/upload-image", "/test.md", "clipboard.png", buildTinyPNGBytes(t))
	if response.Code != http.StatusCreated {
		t.Fatalf("expected status %d, got %d with body %s", http.StatusCreated, response.Code, response.Body.String())
	}

	var payload struct {
		Success      bool   `json:"success"`
		AssetPath    string `json:"assetPath"`
		RelativePath string `json:"relativePath"`
		BytesWritten int64  `json:"bytesWritten"`
		MIMEType     string `json:"mimeType"`
	}
	decodeEditorImageUploadResponse(t, response, &payload)
	if !payload.Success {
		t.Fatalf("expected success payload, got %+v", payload)
	}
	if payload.MIMEType != "image/png" {
		t.Fatalf("expected image/png MIME type, got %+v", payload)
	}
	if !strings.HasPrefix(payload.RelativePath, ".jotes-test.md/pasted-image-") || !strings.HasSuffix(payload.RelativePath, ".png") {
		t.Fatalf("expected markdown relative path inside .jotes-test.md, got %+v", payload)
	}
	if !strings.HasPrefix(payload.AssetPath, "/.jotes-test.md/pasted-image-") || !strings.HasSuffix(payload.AssetPath, ".png") {
		t.Fatalf("expected stored asset path inside /.jotes-test.md, got %+v", payload)
	}
	if payload.BytesWritten <= 0 {
		t.Fatalf("expected positive bytesWritten, got %+v", payload)
	}

	storedFSPath := filepath.Join(rootDir, strings.TrimPrefix(payload.AssetPath, "/"))
	if _, err := os.Stat(filepath.Join(rootDir, ".jotes-test.md")); err != nil {
		t.Fatalf("expected companion directory to exist: %v", err)
	}
	storedBytes, err := os.ReadFile(storedFSPath)
	if err != nil {
		t.Fatalf("read stored image: %v", err)
	}
	if len(storedBytes) == 0 {
		t.Fatalf("expected stored image bytes, got empty file")
	}
}

// TestUploadEditorImageHandlerStoresOrgPastedImage verifies that one pasted
// image uploaded for an Org note is stored beneath the managed sibling .jotes
// directory and returns the expected Org-relative path.
//
// Parameters:
//   - t: the active Go test instance.
//
// Returns:
//   - none: the test fails when the handler does not create the expected managed asset path.
func TestUploadEditorImageHandlerStoresOrgPastedImage(t *testing.T) {
	rootDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(rootDir, "agenda.org"), []byte("* Agenda\n"), 0o644); err != nil {
		t.Fatalf("create org note: %v", err)
	}

	handler := handlers.UploadEditorImageHandler(rootDir)
	response := performEditorImageUploadRequest(t, handler, "/jotes/api/editor/upload-image", "/agenda.org", "clipboard.png", buildTinyPNGBytes(t))
	if response.Code != http.StatusCreated {
		t.Fatalf("expected status %d, got %d with body %s", http.StatusCreated, response.Code, response.Body.String())
	}

	var payload struct {
		RelativePath string `json:"relativePath"`
	}
	decodeEditorImageUploadResponse(t, response, &payload)
	if !strings.HasPrefix(payload.RelativePath, ".jotes-agenda.org/pasted-image-") || !strings.HasSuffix(payload.RelativePath, ".png") {
		t.Fatalf("expected org relative path inside .jotes-agenda.org, got %+v", payload)
	}
}

// TestUploadEditorImageHandlerRejectsNonImagePayload verifies that pasted-image
// uploads reject non-image payloads even when the request otherwise matches the
// editor upload API contract.
//
// Parameters:
//   - t: the active Go test instance.
//
// Returns:
//   - none: the test fails when the handler accepts a non-image upload.
func TestUploadEditorImageHandlerRejectsNonImagePayload(t *testing.T) {
	rootDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(rootDir, "test.md"), []byte("# Title\n"), 0o644); err != nil {
		t.Fatalf("create markdown note: %v", err)
	}

	handler := handlers.UploadEditorImageHandler(rootDir)
	response := performEditorImageUploadRequest(t, handler, "/jotes/api/editor/upload-image", "/test.md", "clipboard.txt", []byte("not-an-image"))
	if response.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("expected status %d, got %d with body %s", http.StatusUnsupportedMediaType, response.Code, response.Body.String())
	}
}

// TestUploadEditorImageHandlerRejectsNonMarkdownOrgNote verifies that pasted
// image uploads are limited to Markdown and Org notes even though other text
// notes remain editable in the browser.
//
// Parameters:
//   - t: the active Go test instance.
//
// Returns:
//   - none: the test fails when the handler accepts a pasted-image upload for an unsupported note type.
func TestUploadEditorImageHandlerRejectsNonMarkdownOrgNote(t *testing.T) {
	rootDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(rootDir, "notes.txt"), []byte("plain text"), 0o644); err != nil {
		t.Fatalf("create text note: %v", err)
	}

	handler := handlers.UploadEditorImageHandler(rootDir)
	response := performEditorImageUploadRequest(t, handler, "/jotes/api/editor/upload-image", "/notes.txt", "clipboard.png", buildTinyPNGBytes(t))
	if response.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("expected status %d, got %d with body %s", http.StatusUnsupportedMediaType, response.Code, response.Body.String())
	}
}

// TestUploadEditorImageHandlerRejectsSpoofedDeclaredImageType verifies that the
// pasted-image upload endpoint trusts the server-side file sniff rather than a
// spoofed multipart Content-Type header supplied by the browser.
//
// Parameters:
//   - t: the active Go test instance.
//
// Returns:
//   - none: the test fails when a non-image payload is accepted solely because the multipart part declared an image content type.
func TestUploadEditorImageHandlerRejectsSpoofedDeclaredImageType(t *testing.T) {
	rootDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(rootDir, "test.md"), []byte("# Title\n"), 0o644); err != nil {
		t.Fatalf("create markdown note: %v", err)
	}

	handler := handlers.UploadEditorImageHandler(rootDir)
	response := performEditorImageUploadRequestWithDeclaredType(t, handler, "/jotes/api/editor/upload-image", "/test.md", "clipboard.png", "image/png", []byte("not-an-image"))
	if response.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("expected status %d, got %d with body %s", http.StatusUnsupportedMediaType, response.Code, response.Body.String())
	}
}

// TestSaveEditorHandlerAcceptsPortlessOriginWithMatchingReferer verifies that
// browser save requests still succeed when an upstream proxy normalizes the
// Origin header to omit the non-default port, provided the same request carries
// a fully matching same-origin Referer.
//
// Parameters:
//   - t: the active Go test instance.
//
// Returns:
//   - none: the test fails when the save handler rejects the reported Origin/Referer header shape.
func TestSaveEditorHandlerAcceptsPortlessOriginWithMatchingReferer(t *testing.T) {
	rootDir := t.TempDir()
	originalContent := "# Title\n"
	updatedContent := "# Updated\n"
	if err := os.WriteFile(filepath.Join(rootDir, "note.md"), []byte(originalContent), 0o644); err != nil {
		t.Fatalf("create editable note: %v", err)
	}

	handler := handlers.SaveEditorHandler(rootDir)
	response := performEditorSaveRequest(t, handler, "/jotes/api/save", map[string]string{
		"path":     "/note.md",
		"content":  updatedContent,
		"revision": buildEditorRevisionToken(originalContent),
	}, map[string]string{
		"Host":    "stink:1314",
		"Origin":  "http://stink",
		"Referer": "http://stink:1314/jotes/edit/note.md",
	})
	if response.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d with body %s", http.StatusOK, response.Code, response.Body.String())
	}

	storedContent, err := os.ReadFile(filepath.Join(rootDir, "note.md"))
	if err != nil {
		t.Fatalf("read updated note: %v", err)
	}
	if string(storedContent) != updatedContent {
		t.Fatalf("expected updated note content %q, got %q", updatedContent, string(storedContent))
	}
}

// TestSaveEditorHandlerRejectsPortlessOriginWithoutMatchingReferer verifies
// that the save handler does not broadly trust an Origin header whose hostname
// matches but whose explicit port was omitted when no exact same-origin Referer
// confirms the request target.
//
// Parameters:
//   - t: the active Go test instance.
//
// Returns:
//   - none: the test fails when the save handler accepts a portless Origin without a matching Referer.
func TestSaveEditorHandlerRejectsPortlessOriginWithoutMatchingReferer(t *testing.T) {
	rootDir := t.TempDir()
	originalContent := "# Title\n"
	if err := os.WriteFile(filepath.Join(rootDir, "note.md"), []byte(originalContent), 0o644); err != nil {
		t.Fatalf("create editable note: %v", err)
	}

	handler := handlers.SaveEditorHandler(rootDir)
	response := performEditorSaveRequest(t, handler, "/jotes/api/save", map[string]string{
		"path":     "/note.md",
		"content":  "# Updated\n",
		"revision": buildEditorRevisionToken(originalContent),
	}, map[string]string{
		"Host":   "stink:1314",
		"Origin": "http://stink",
	})
	if response.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d with body %s", http.StatusBadRequest, response.Code, response.Body.String())
	}
}

// TestSaveEditorHandlerRejectsWrongExplicitOriginPort verifies that the save
// handler still rejects requests whose Origin explicitly targets a different
// port even when the Referer matches the active editor page.
//
// Parameters:
//   - t: the active Go test instance.
//
// Returns:
//   - none: the test fails when the save handler accepts an Origin with the wrong explicit port.
func TestSaveEditorHandlerRejectsWrongExplicitOriginPort(t *testing.T) {
	rootDir := t.TempDir()
	originalContent := "# Title\n"
	if err := os.WriteFile(filepath.Join(rootDir, "note.md"), []byte(originalContent), 0o644); err != nil {
		t.Fatalf("create editable note: %v", err)
	}

	handler := handlers.SaveEditorHandler(rootDir)
	response := performEditorSaveRequest(t, handler, "/jotes/api/save", map[string]string{
		"path":     "/note.md",
		"content":  "# Updated\n",
		"revision": buildEditorRevisionToken(originalContent),
	}, map[string]string{
		"Host":    "stink:1314",
		"Origin":  "http://stink:9999",
		"Referer": "http://stink:1314/jotes/edit/note.md",
	})
	if response.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d with body %s", http.StatusBadRequest, response.Code, response.Body.String())
	}
}

// TestEditHandlerRejectsManagedJotesCompanionTextFile verifies that text files
// stored inside managed .jotes folders cannot be opened in the browser editor.
//
// Parameters:
//   - t: the active Go test instance.
//
// Returns:
//   - none: the test fails when EditHandler accepts a managed .jotes text file.
func TestEditHandlerRejectsManagedJotesCompanionTextFile(t *testing.T) {
	rootDir := t.TempDir()
	if err := os.Mkdir(filepath.Join(rootDir, ".jotes-note.md"), 0o755); err != nil {
		t.Fatalf("create companion directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(rootDir, ".jotes-note.md", "nested.md"), []byte("# Managed\n"), 0o644); err != nil {
		t.Fatalf("create nested markdown file: %v", err)
	}

	handler := handlers.EditHandler(rootDir, "Jotes", "catppuccin", &noopEditorTemplate{})
	request := httptest.NewRequest(http.MethodGet, "/.jotes-note.md/nested.md", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("expected status %d, got %d with body %s", http.StatusUnsupportedMediaType, response.Code, response.Body.String())
	}
}

// TestPreviewDisablesEditingForManagedJotesCompanionTextFile verifies that the
// normal file preview page does not offer browser editing for text files stored
// inside managed .jotes folders.
//
// Parameters:
//   - t: the active Go test instance.
//
// Returns:
//   - none: the test fails when preview rendering marks a managed .jotes text file as editable.
func TestPreviewDisablesEditingForManagedJotesCompanionTextFile(t *testing.T) {
	rootDir := t.TempDir()
	if err := os.Mkdir(filepath.Join(rootDir, ".jotes-note.md"), 0o755); err != nil {
		t.Fatalf("create companion directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(rootDir, ".jotes-note.md", "nested.md"), []byte("# Managed\n"), 0o644); err != nil {
		t.Fatalf("create nested markdown file: %v", err)
	}

	templateCapture := &capturingPreviewTemplate{}
	handler := handlers.UniversalHandler(rootDir, "Jotes", "catppuccin", templateCapture)
	request := httptest.NewRequest(http.MethodGet, "/.jotes-note.md/nested.md", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d with body %s", http.StatusOK, response.Code, response.Body.String())
	}
	if templateCapture.preview == nil {
		t.Fatalf("expected captured preview data")
	}
	if templateCapture.preview.CanEdit {
		t.Fatalf("expected managed .jotes preview to disable editing, got %+v", templateCapture.preview)
	}
}
