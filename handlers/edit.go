package handlers

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"html/template"
	"io"
	stdmime "mime"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"

	"jotes/models"
)

const (
	maxEditableNoteBytes  = 2 * 1024 * 1024
	maxEditorRequestBytes = 4 * 1024 * 1024
)

var (
	errEditorUnsupportedFile = errors.New("the browser editor only supports text notes")
	errEditorFileTooLarge    = errors.New("the requested note is too large for the browser editor")
	errEditorConflict        = errors.New("the requested note changed on disk before it could be saved")
)

type editorRequest struct {
	Path     string `json:"path"`
	Content  string `json:"content"`
	Revision string `json:"revision,omitempty"`
}

type editorRenderResponse struct {
	HTML string `json:"html"`
}

type editorSaveResponse struct {
	Saved    bool   `json:"saved"`
	Message  string `json:"message"`
	Revision string `json:"revision"`
}

type editorErrorResponse struct {
	Error string `json:"error"`
}

// EditHandler renders the dedicated note-editing page for one supported text note.
//
// Parameters:
//   - rootDir: the single filesystem directory exposed by the server.
//   - siteName: the branding label shown in page titles and breadcrumbs.
//   - defaultTheme: the HTML theme class applied to the page root.
//   - tmpl: any renderer capable of executing the editor template.
//
// Returns:
//   - http.HandlerFunc: a handler that loads the current file contents and renders the editor UI.
func EditHandler(rootDir, siteName, defaultTheme string, tmpl interface {
	ExecuteEditor(http.ResponseWriter, *models.EditorData) error
}) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		urlPath, fsPath, mimeType, err := resolveEditableFile(rootDir, r.URL.Path)
		if err != nil {
			switch {
			case errors.Is(err, errEditorUnsupportedFile):
				http.Error(w, "This file cannot be edited in Jotes", http.StatusUnsupportedMediaType)
			default:
				http.Error(w, "Not found", http.StatusNotFound)
			}
			return
		}

		content, err := readEditableTextFile(fsPath)
		if err != nil {
			switch {
			case errors.Is(err, errEditorFileTooLarge):
				http.Error(w, "This note is too large to edit in the browser", http.StatusRequestEntityTooLarge)
			default:
				http.Error(w, "Could not read file", http.StatusInternalServerError)
			}
			return
		}

		renderMode := previewModeForTextNote(mimeType)
		renderedContent := template.HTML("")
		if renderMode == "document" {
			renderedContent, err = buildEditorPreviewHTML(content, mimeType, urlPath)
			if err != nil {
				renderedContent = buildRenderStatusMessageHTML("render-preview-error", "Rendered preview is unavailable for this document right now.")
			}
		}

		w.Header().Set("Cache-Control", "no-store")

		data := &models.EditorData{
			Title:            filepath.Base(fsPath),
			SiteName:         siteName,
			CurrentUser:      currentUserViewFromRequest(r),
			ShowSearch:       true,
			DefaultTheme:     defaultTheme,
			FilePath:         urlPath,
			FileName:         filepath.Base(fsPath),
			MIMEType:         mimeType,
			BackURL:          urlPath,
			SaveURL:          "/jotes/api/save",
			RenderURL:        "/jotes/api/render",
			ImageUploadURL:   "/jotes/api/editor/upload-image",
			Revision:         buildContentRevision(content),
			PlaintextContent: content,
			RenderMode:       renderMode,
			RenderedContent:  renderedContent,
			Breadcrumbs:      buildFileBreadcrumbs(siteName, urlPath),
		}

		if err := tmpl.ExecuteEditor(w, data); err != nil {
			http.Error(w, "Template error", http.StatusInternalServerError)
		}
	}
}

// RenderEditorHandler produces the live rendered preview fragment for the note
// editor without persisting any file changes.
//
// Parameters:
//   - rootDir: the single filesystem directory exposed by the server, used to validate the requested note path.
//
// Returns:
//   - http.HandlerFunc: a JSON API handler that renders the supplied note text and returns an HTML fragment for Markdown, Org, and HTML notes.
func RenderEditorHandler(rootDir string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		req, err := decodeEditorRequest(w, r)
		if err != nil {
			writeEditorRequestError(w, err)
			return
		}

		urlPath, _, mimeType, err := resolveEditableFile(rootDir, req.Path)
		if err != nil {
			writeEditorRequestError(w, err)
			return
		}
		if previewModeForTextNote(mimeType) != "document" {
			writeEditorJSON(w, http.StatusBadRequest, editorErrorResponse{Error: "This note uses a local CodeMirror preview and does not require server rendering"})
			return
		}

		html, err := buildEditorPreviewHTML(req.Content, mimeType, urlPath)
		if err != nil {
			writeEditorJSON(w, http.StatusBadRequest, editorErrorResponse{Error: "Jotes could not render the current note text"})
			return
		}

		writeEditorJSON(w, http.StatusOK, editorRenderResponse{HTML: string(html)})
	}
}

// SaveEditorHandler writes the current plaintext editor contents back to the
// underlying filesystem note when the on-disk revision still matches what the
// editor originally loaded.
//
// Parameters:
//   - rootDir: the single filesystem directory exposed by the server, used to validate and resolve the note path.
//
// Returns:
//   - http.HandlerFunc: a JSON API handler that persists the supplied note text when the request is valid.
func SaveEditorHandler(rootDir string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var newRevision string

		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		if err := validateEditorWriteRequest(r); err != nil {
			writeEditorRequestError(w, err)
			return
		}

		req, err := decodeEditorRequest(w, r)
		if err != nil {
			writeEditorRequestError(w, err)
			return
		}
		if strings.TrimSpace(req.Revision) == "" {
			writeEditorJSON(w, http.StatusBadRequest, editorErrorResponse{Error: "Missing note revision for save request"})
			return
		}

		if _, _, _, err := resolveEditableFile(rootDir, req.Path); err != nil {
			writeEditorRequestError(w, err)
			return
		}

		newRevision, err = saveEditableTextFile(path.Clean("/"+req.Path), rootDir, req.Revision, req.Content)
		if err != nil {
			writeEditorRequestError(w, err)
			return
		}

		writeEditorJSON(w, http.StatusOK, editorSaveResponse{
			Saved:    true,
			Message:  "Saved",
			Revision: newRevision,
		})
	}
}

// resolveEditableFile validates one editor target path, resolves it against the
// configured root directory, and verifies that it is a supported note file.
//
// Parameters:
//   - rootDir: the single filesystem directory exposed by the server.
//   - rawPath: the request-supplied note path, with or without a leading slash.
//
// Returns:
//   - string: the cleaned URL path beginning with /.
//   - string: the resolved filesystem path under rootDir.
//   - string: the detected MIME type for the target file.
//   - error: non-nil when the path cannot be resolved, does not exist, is a directory, or is not an editable note.
func resolveEditableFile(rootDir, rawPath string) (string, string, string, error) {
	urlPath := path.Clean("/" + rawPath)
	fsPath, err := resolvePath(rootDir, urlPath)
	if err != nil {
		return "", "", "", err
	}

	info, err := os.Stat(fsPath)
	if err != nil {
		return "", "", "", err
	}
	if info.IsDir() {
		return "", "", "", os.ErrNotExist
	}

	mimeType := mimeForFile(fsPath)
	if !isEditableNoteFile(urlPath, mimeType) {
		return "", "", "", errEditorUnsupportedFile
	}

	return urlPath, fsPath, mimeType, nil
}

// isEditableNoteFile reports whether a path and MIME type combination should be
// editable through the browser editor.
//
// Parameters:
//   - urlPath: the cleaned note URL path whose file name can provide extension-based editor mode hints for known textual formats.
//   - mimeType: the detected MIME type used to confirm the file is textual.
//
// Returns:
//   - bool: true when the target file should be handled as text in the editor, otherwise false; managed .jotes companion content is always rejected.
func isEditableNoteFile(urlPath, mimeType string) bool {
	if !isText(mimeType) {
		return false
	}
	if pathContainsJotesCompanionDirectory(urlPath) {
		return false
	}

	cleanName := strings.ToLower(filepath.Base(urlPath))
	if cleanName == "" || cleanName == "." || cleanName == "/" {
		return false
	}

	return true
}

// readEditableTextFile loads one note for browser editing while rejecting any
// content that exceeds the editor's configured size limit.
//
// Parameters:
//   - fsPath: the filesystem path of the note that should be read.
//
// Returns:
//   - string: the complete note contents when the file fits within the editor size limit.
//   - error: non-nil when the file is too large or cannot be read.
func readEditableTextFile(fsPath string) (string, error) {
	file, err := os.Open(fsPath)
	if err != nil {
		return "", err
	}
	defer file.Close()

	data, err := io.ReadAll(io.LimitReader(file, maxEditableNoteBytes+1))
	if err != nil {
		return "", err
	}
	if len(data) > maxEditableNoteBytes {
		return "", errEditorFileTooLarge
	}
	return string(data), nil
}

// saveEditableTextFile serializes one Jotes save operation behind a lock file,
// verifies the expected on-disk revision before and after preparing the
// replacement bytes, and commits the new note body with an atomic rename.
//
// Parameters:
//   - rawPath: the cleaned URL path of the note being saved.
//   - rootDir: the single filesystem directory exposed by the server.
//   - expectedRevision: the content hash that the editor originally loaded.
//   - content: the new plaintext note body to persist.
//
// Returns:
//   - string: the revision hash for the newly saved content.
//   - error: non-nil when the note cannot be opened, verified, or replaced safely.
func saveEditableTextFile(rawPath, rootDir, expectedRevision, content string) (string, error) {
	fsPath, err := resolvePath(rootDir, rawPath)
	if err != nil {
		return "", err
	}

	lockFile, err := openEditorLockFile(fsPath)
	if err != nil {
		return "", err
	}
	defer lockFile.Close()
	defer unlockOpenFile(lockFile)

	currentContent, err := readEditableTextFile(fsPath)
	if err != nil {
		return "", err
	}
	if buildContentRevision(currentContent) != expectedRevision {
		return "", errEditorConflict
	}

	info, err := os.Stat(fsPath)
	if err != nil {
		return "", err
	}

	tempPath, err := createAtomicReplacement(fsPath, info.Mode(), content)
	if err != nil {
		return "", err
	}
	defer os.Remove(tempPath)

	currentContent, err = readEditableTextFile(fsPath)
	if err != nil {
		return "", err
	}
	if buildContentRevision(currentContent) != expectedRevision {
		return "", errEditorConflict
	}

	if err := commitAtomicReplacement(tempPath, fsPath); err != nil {
		return "", err
	}

	invalidateDirectoryListings()
	return buildContentRevision(content), nil
}

// openEditorLockFile opens or creates the cross-request lock file that Jotes
// uses to serialize save attempts for one note path.
//
// Parameters:
//   - fsPath: the resolved filesystem path of the note whose save operation should be serialized.
//
// Returns:
//   - *os.File: an open, exclusively locked file handle that must be unlocked and closed by the caller.
//   - error: non-nil when the lock file cannot be created or locked.
func openEditorLockFile(fsPath string) (*os.File, error) {
	lockPath := editorLockPath(fsPath)
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o755); err != nil {
		return nil, err
	}

	file, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := lockOpenFile(file); err != nil {
		file.Close()
		return nil, err
	}

	return file, nil
}

// editorLockPath maps one note filesystem path onto the shared operating-system
// temporary directory location used for Jotes save locks.
//
// Parameters:
//   - fsPath: the resolved filesystem path of the note being locked.
//
// Returns:
//   - string: the absolute path of the lock file that should serialize saves for fsPath.
func editorLockPath(fsPath string) string {
	return filepath.Join(os.TempDir(), "jotes-locks", buildContentRevision(fsPath)+".lock")
}

// createAtomicReplacement writes the new note body into a temporary file in the
// destination directory, applies the original permission bits, and fsyncs the
// temporary file so it is ready for an atomic rename.
//
// Parameters:
//   - fsPath: the final filesystem path that will later receive the replacement file.
//   - mode: the permission bits copied from the current on-disk note.
//   - content: the new plaintext note body to store in the temporary file.
//
// Returns:
//   - string: the absolute temporary-file path that should later be renamed into place.
//   - error: non-nil when the temporary file cannot be created, written, or synced.
func createAtomicReplacement(fsPath string, mode os.FileMode, content string) (string, error) {
	dirPath := filepath.Dir(fsPath)
	tempFile, err := os.CreateTemp(dirPath, ".jotes-edit-*")
	if err != nil {
		return "", err
	}

	tempPath := tempFile.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = tempFile.Close()
			_ = os.Remove(tempPath)
		}
	}()

	if err := tempFile.Chmod(mode.Perm()); err != nil {
		return "", err
	}
	if _, err := io.WriteString(tempFile, content); err != nil {
		return "", err
	}
	if err := tempFile.Sync(); err != nil {
		return "", err
	}
	if err := tempFile.Close(); err != nil {
		return "", err
	}

	cleanup = false
	return tempPath, nil
}

// commitAtomicReplacement renames a fully prepared temporary note file over the
// live note path and fsyncs the parent directory so the rename is crash-safe.
//
// Parameters:
//   - tempPath: the temporary file path returned by createAtomicReplacement.
//   - finalPath: the destination note path that should be atomically replaced.
//
// Returns:
//   - error: non-nil when the rename or directory sync fails.
func commitAtomicReplacement(tempPath, finalPath string) error {
	if err := os.Rename(tempPath, finalPath); err != nil {
		return err
	}
	return syncDirectory(filepath.Dir(finalPath))
}

// decodeEditorRequest parses one JSON request body used by the editor render
// and save API endpoints.
//
// Parameters:
//   - w: the HTTP response writer used to enforce the maximum request body size.
//   - r: the inbound HTTP request whose body should contain a JSON object with path, content, and optional revision fields.
//
// Returns:
//   - editorRequest: the decoded editor payload.
//   - error: non-nil when the body is too large, malformed, missing a target path, or exceeds the supported note size.
func decodeEditorRequest(w http.ResponseWriter, r *http.Request) (editorRequest, error) {
	r.Body = http.MaxBytesReader(w, r.Body, maxEditorRequestBytes)

	var req editorRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		return editorRequest{}, err
	}
	if req.Path == "" {
		return editorRequest{}, errors.New("missing editor path")
	}
	if len(req.Content) > maxEditableNoteBytes {
		return editorRequest{}, errEditorFileTooLarge
	}
	return req, nil
}

// validateEditorMutationRequest rejects note-mutation requests that do not
// look like same-origin calls generated by the Jotes editor UI.
//
// Parameters:
//   - r: the incoming HTTP request that intends to save note text or upload pasted note assets.
//
// Returns:
//   - error: non-nil when the request is missing the editor header or presents a mismatched origin or referer.
func validateEditorMutationRequest(r *http.Request) error {
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	referer := strings.TrimSpace(r.Header.Get("Referer"))

	if !strings.EqualFold(strings.TrimSpace(r.Header.Get("X-Jotes-Editor")), "1") {
		return errors.New("missing editor header")
	}

	if referer != "" && !requestURLMatchesHost(referer, r.Host) {
		return errors.New("editor request referer did not match the active Jotes host")
	}
	if origin != "" && !requestURLMatchesHost(origin, r.Host) {
		if referer == "" || requestURLPort(origin) != "" || !requestURLMatchesHostName(origin, r.Host) {
			return errors.New("editor request origin did not match the active Jotes host")
		}
	}

	return nil
}

// validateEditorWriteRequest rejects save requests that do not look like the
// same-origin JSON calls generated by the Jotes editor UI.
//
// Parameters:
//   - r: the incoming HTTP request that intends to mutate a note on disk.
//
// Returns:
//   - error: non-nil when the request fails the shared editor checks or uses the wrong content type.
func validateEditorWriteRequest(r *http.Request) error {
	if err := validateEditorMutationRequest(r); err != nil {
		return err
	}

	mediaType, _, err := stdmime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		return errors.New("editor save requests must use application/json")
	}

	return nil
}

// validateEditorMultipartWriteRequest rejects pasted-image upload requests that
// do not look like same-origin multipart calls generated by the Jotes editor
// UI.
//
// Parameters:
//   - r: the incoming HTTP request that intends to upload pasted note assets.
//
// Returns:
//   - error: non-nil when the request fails the shared editor checks or does not use multipart/form-data.
func validateEditorMultipartWriteRequest(r *http.Request) error {
	if err := validateEditorMutationRequest(r); err != nil {
		return err
	}

	mediaType, _, err := stdmime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "multipart/form-data" {
		return errors.New("editor image uploads must use multipart/form-data")
	}

	return nil
}

// requestURLMatchesHost reports whether a request Origin or Referer URL points
// at the same hostname and explicit port combination that served the current
// Jotes request.
//
// Parameters:
//   - rawURL: the raw Origin or Referer header value to examine.
//   - host: the Host header from the current HTTP request.
//
// Returns:
//   - bool: true when rawURL parses successfully and matches host with the same hostname and explicit port.
func requestURLMatchesHost(rawURL, host string) bool {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return false
	}

	requestHostName := requestHostName(host)
	requestPort := requestHostPort(host)
	if requestHostName == "" || !strings.EqualFold(parsed.Hostname(), requestHostName) {
		return false
	}

	return parsed.Port() == requestPort
}

// requestURLMatchesHostName reports whether a request Origin or Referer URL
// belongs to the same hostname as the current Jotes request, ignoring port
// differences.
//
// Parameters:
//   - rawURL: the raw Origin or Referer header value to examine.
//   - host: the Host header from the current HTTP request.
//
// Returns:
//   - bool: true when rawURL parses successfully and its hostname matches host, even if the URL omitted an explicit port.
func requestURLMatchesHostName(rawURL, host string) bool {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return false
	}

	requestHostName := requestHostName(host)
	return requestHostName != "" && strings.EqualFold(parsed.Hostname(), requestHostName)
}

// requestURLPort extracts the explicit port from one request Origin or Referer
// URL so editor validation can distinguish a hostname-only URL from one that
// deliberately targets a different port.
//
// Parameters:
//   - rawURL: the raw Origin or Referer header value to examine.
//
// Returns:
//   - string: the explicit port substring without separators, or an empty string when rawURL omits a port or cannot be parsed.
func requestURLPort(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}

	return parsed.Port()
}

// requestHostName extracts the hostname portion from one HTTP Host header
// value, tolerating both host:port pairs and bare hostnames.
//
// Parameters:
//   - host: the raw Host header value from the current HTTP request.
//
// Returns:
//   - string: the normalized hostname without any trailing port, or an empty string when host is blank.
func requestHostName(host string) string {
	host = strings.TrimSpace(host)
	if host == "" {
		return ""
	}

	parsedHost := host
	if candidateHost, _, err := net.SplitHostPort(host); err == nil {
		parsedHost = candidateHost
	}
	if strings.HasPrefix(parsedHost, "[") && strings.HasSuffix(parsedHost, "]") {
		parsedHost = strings.TrimPrefix(strings.TrimSuffix(parsedHost, "]"), "[")
	}

	return parsedHost
}

// requestHostPort extracts the explicit port portion from one HTTP Host header
// value, returning an empty string when the current request host omitted a
// port.
//
// Parameters:
//   - host: the raw Host header value from the current HTTP request.
//
// Returns:
//   - string: the port substring without separators, or an empty string when host does not specify one.
func requestHostPort(host string) string {
	host = strings.TrimSpace(host)
	if host == "" {
		return ""
	}

	if strings.Contains(host, ":") {
		if _, port, err := net.SplitHostPort(host); err == nil {
			return port
		}
	}

	return ""
}

// buildContentRevision creates a stable revision token for one note body so
// save requests can detect concurrent on-disk edits.
//
// Parameters:
//   - content: the exact note text whose revision token should be generated.
//
// Returns:
//   - string: a hexadecimal SHA-256 digest for the supplied content.
func buildContentRevision(content string) string {
	digest := sha256.Sum256([]byte(content))
	return hex.EncodeToString(digest[:])
}

// previewModeForTextNote decides which rendered-preview implementation one text
// note should use in preview and editor contexts.
//
// Parameters:
//   - mimeType: the detected MIME type for the note being previewed.
//
// Returns:
//   - string: "document" for Markdown, Org, and HTML notes, or "codemirror" for all other text notes.
func previewModeForTextNote(mimeType string) string {
	if isRenderable(mimeType) {
		return "document"
	}

	return "codemirror"
}

// buildEditorPreviewHTML converts the current plaintext editor contents into
// the HTML fragment shown in the editor's rendered-preview tab for rich
// document notes.
//
// Parameters:
//   - content: the raw note text currently present in the editor.
//   - mimeType: the detected MIME type used to choose the correct rich renderer.
//   - docURLPath: the preview URL path of the note being edited, used for relative asset resolution inside rendered output.
//
// Returns:
//   - template.HTML: a safe HTML fragment ready to inject into the editor preview container.
//   - error: non-nil when the selected rich renderer fails to process the supplied content.
func buildEditorPreviewHTML(content, mimeType, docURLPath string) (template.HTML, error) {
	if strings.TrimSpace(content) == "" {
		return template.HTML(`<p class="editor-render-empty">This note is empty.</p>`), nil
	}

	return renderContent(content, mimeType, docURLPath)
}

// writeEditorRequestError maps one editor validation error into the HTTP status
// code and JSON payload that the browser-facing APIs should return.
//
// Parameters:
//   - w: the HTTP response writer that should receive the error response.
//   - err: the validation or resolution failure that occurred while handling the request.
//
// Returns:
//   - none: the translated error response is written directly to w.
func writeEditorRequestError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, errEditorUnsupportedFile):
		writeEditorJSON(w, http.StatusUnsupportedMediaType, editorErrorResponse{Error: "This file cannot be edited in Jotes"})
	case errors.Is(err, errEditorFileTooLarge):
		writeEditorJSON(w, http.StatusRequestEntityTooLarge, editorErrorResponse{Error: "This note is too large to edit in the browser"})
	case errors.Is(err, errEditorConflict):
		writeEditorJSON(w, http.StatusConflict, editorErrorResponse{Error: "This note changed on disk. Reload the editor before saving again."})
	case errors.Is(err, os.ErrNotExist):
		writeEditorJSON(w, http.StatusNotFound, editorErrorResponse{Error: "The requested file could not be found"})
	default:
		writeEditorJSON(w, http.StatusBadRequest, editorErrorResponse{Error: "Invalid editor request payload"})
	}
}

// writeEditorJSON serializes one editor API response as JSON with no-store
// caching semantics.
//
// Parameters:
//   - w: the HTTP response writer that should receive the JSON payload.
//   - status: the HTTP status code to send before the encoded response body.
//   - payload: any JSON-serializable value describing the API result.
//
// Returns:
//   - none: the response headers and body are written directly to w.
func writeEditorJSON(w http.ResponseWriter, status int, payload interface{}) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
