package test

import (
	"bytes"
	"encoding/json"
	"html/template"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"jotes/handlers"
	"jotes/models"
)

// capturePreviewTemplate records the last preview view-model passed to it so
// handler tests can assert preview classification without depending on the
// repository's full HTML template execution path.
type capturePreviewTemplate struct {
	lastData *models.PreviewData
}

// ExecutePreview stores the supplied preview data and mimics a successful
// preview render for handler-focused tests.
//
// Parameters:
//   - w: the HTTP response writer receiving the synthetic preview response.
//   - data: the preview data built by the handler under test.
//
// Returns:
//   - error: always nil because the capture template never fails intentionally.
func (c *capturePreviewTemplate) ExecutePreview(w http.ResponseWriter, data *models.PreviewData) error {
	c.lastData = data
	w.WriteHeader(http.StatusOK)
	return nil
}

// testAddInt mirrors the production template helper closely enough for preview
// template tests that only need integer breadcrumb arithmetic.
//
// Parameters:
//   - a: the first integer operand.
//   - b: the second integer operand.
//
// Returns:
//   - int: the sum of a and b.
func testAddInt(a, b int) int {
	return a + b
}

// testToJSON mirrors the production template helper closely enough for preview
// template tests that need inline JSON script tags to render successfully.
//
// Parameters:
//   - value: the Go value that should be serialized into JSON.
//
// Returns:
//   - template.JS: the serialized JSON marked safe for the test template.
//   - error: non-nil when JSON marshaling fails.
func testToJSON(value interface{}) (template.JS, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return template.JS(encoded), nil
}

// testAvatarDataURI supplies a harmless placeholder avatar URI so the shared
// base template can execute during preview HTML assertions.
//
// Parameters:
//   - username: the user name whose avatar would normally be generated.
//
// Returns:
//   - template.URL: a minimal data URI placeholder used only in tests.
func testAvatarDataURI(username string) template.URL {
	return template.URL("data:image/svg+xml,")
}

// loadPreviewTemplateFromRepository parses the real base and file-preview
// templates from disk using a minimal test-only function map so tests can
// assert the final HTML emitted for PDF previews.
//
// Parameters:
//   - t: the active Go test instance used for helper failure reporting.
//
// Returns:
//   - *template.Template: the parsed template set ready to execute the "base" template.
func loadPreviewTemplateFromRepository(t *testing.T) *template.Template {
	t.Helper()

	funcs := template.FuncMap{
		"add":           testAddInt,
		"toJSON":        testToJSON,
		"avatarDataURI": testAvatarDataURI,
	}

	tmpl := template.New("").Funcs(funcs)
	parsed, err := tmpl.ParseFiles(
		filepath.Join("..", "templates", "base.html"),
		filepath.Join("..", "templates", "file-preview.html"),
	)
	if err != nil {
		t.Fatalf("parse preview templates: %v", err)
	}

	return parsed
}

// TestViewHandlerServesPDFWithApplicationPDF verifies that inline file serving
// returns application/pdf for .pdf files so strict nosniff browsers can load
// them inside the bundled PDF.js preview.
//
// Parameters:
//   - t: the active Go test instance.
//
// Returns:
//   - none: the test fails when the served MIME type is not application/pdf.
func TestViewHandlerServesPDFWithApplicationPDF(t *testing.T) {
	rootDir := t.TempDir()
	pdfPath := filepath.Join(rootDir, "manual.pdf")
	if err := os.WriteFile(pdfPath, []byte("%PDF-1.4\n1 0 obj\n<<>>\nendobj\ntrailer\n<<>>\n%%EOF\n"), 0o644); err != nil {
		t.Fatalf("write pdf fixture: %v", err)
	}

	handler := handlers.ViewHandler(rootDir)
	request := httptest.NewRequest(http.MethodGet, "/manual.pdf", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d with body %s", http.StatusOK, response.Code, response.Body.String())
	}
	if contentType := response.Header().Get("Content-Type"); !strings.HasPrefix(contentType, "application/pdf") {
		t.Fatalf("expected application/pdf content type, got %q", contentType)
	}
}

// TestPreviewHandlerClassifiesPDFAsReadOnlyPreview verifies that PDF files flow
// through the preview pipeline, expose a /view URL for PDF.js, and remain
// non-editable in the GUI.
//
// Parameters:
//   - t: the active Go test instance.
//
// Returns:
//   - none: the test fails when the preview handler does not classify the PDF correctly.
func TestPreviewHandlerClassifiesPDFAsReadOnlyPreview(t *testing.T) {
	rootDir := t.TempDir()
	pdfDir := filepath.Join(rootDir, "docs")
	if err := os.MkdirAll(pdfDir, 0o755); err != nil {
		t.Fatalf("create docs dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(pdfDir, "guide.pdf"), []byte("%PDF-1.4\n%%EOF\n"), 0o644); err != nil {
		t.Fatalf("write pdf fixture: %v", err)
	}

	captured := &capturePreviewTemplate{}
	handler := handlers.PreviewHandler(rootDir, "Jotes", "dark", captured)
	request := httptest.NewRequest(http.MethodGet, "/docs/guide.pdf", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d with body %s", http.StatusOK, response.Code, response.Body.String())
	}
	if captured.lastData == nil {
		t.Fatal("expected preview data to be captured")
	}
	if !captured.lastData.IsPDF {
		t.Fatal("expected preview data to mark the file as a PDF preview")
	}
	if captured.lastData.CanEdit {
		t.Fatal("expected PDF preview to remain non-editable")
	}
	if captured.lastData.ViewURL != "/view/docs/guide.pdf" {
		t.Fatalf("expected /view URL for PDF preview, got %q", captured.lastData.ViewURL)
	}
	if captured.lastData.MIMEType != "application/pdf" {
		t.Fatalf("expected MIME type application/pdf, got %q", captured.lastData.MIMEType)
	}
}

// TestPreviewTemplateRendersBundledPDFViewer verifies that the real preview
// template emits the offline PDF.js assets, omits the Edit action for read-only
// PDF previews, and does not render the standalone metadata card above the viewer.
//
// Parameters:
//   - t: the active Go test instance.
//
// Returns:
//   - none: the test fails when the HTML output omits the PDF preview shell,
//     exposes an Edit action, or includes the removed top metadata card.
func TestPreviewTemplateRendersBundledPDFViewer(t *testing.T) {
	tmpl := loadPreviewTemplateFromRepository(t)
	previewData := &models.PreviewData{
		Title:        "guide.pdf",
		SiteName:     "Jotes",
		DefaultTheme: "dark",
		FilePath:     "/docs/guide.pdf",
		FileName:     "guide.pdf",
		IsPDF:        true,
		CanEdit:      false,
		ViewURL:      "/view/docs/guide.pdf",
		MIMEType:     "application/pdf",
		Breadcrumbs: []models.Breadcrumb{
			{Name: "Jotes", Path: "/"},
			{Name: "docs", Path: "/docs"},
			{Name: "guide.pdf", Path: "/docs/guide.pdf"},
		},
	}

	var output bytes.Buffer
	if err := tmpl.ExecuteTemplate(&output, "base", previewData); err != nil {
		t.Fatalf("execute preview template: %v", err)
	}

	html := output.String()
	if !strings.Contains(html, "data-pdf-preview-root=\"1\"") {
		t.Fatal("expected preview HTML to include the PDF preview root")
	}
	if !strings.Contains(html, "/static/js/pdf-preview.js") {
		t.Fatal("expected preview HTML to load the bundled PDF preview script")
	}
	if !strings.Contains(html, "/static/pdfjs/pdf.worker.mjs") {
		t.Fatal("expected preview HTML to reference the bundled PDF.js worker path")
	}
	if strings.Contains(html, "info-card info-card--inline") {
		t.Fatal("expected preview HTML to omit the top PDF metadata card")
	}
	if strings.Contains(html, ">Edit<") {
		t.Fatal("expected preview HTML to omit the Edit action for PDFs")
	}
}
