package handlers

import (
	"bytes"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"

	chromahtml "github.com/alecthomas/chroma/v2/formatters/html"
	"github.com/alecthomas/chroma/v2/styles"

	"jotes/models"
)

// PreviewHandler renders the preview page for a directory, image, text file,
// PDF document, or generic binary file.
//
// Parameters:
//   - rootDir: the single filesystem directory exposed by the server.
//   - siteName: the branding label shown in page titles and breadcrumbs.
//   - defaultTheme: the HTML theme class applied to the page root.
//   - tmpl: any renderer capable of executing the preview template.
//
// Returns:
//   - http.HandlerFunc: a handler that resolves the request path and renders the correct preview mode.
func PreviewHandler(rootDir, siteName, defaultTheme string, tmpl interface {
	ExecutePreview(http.ResponseWriter, *models.PreviewData) error
}) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		urlPath := path.Clean("/" + r.URL.Path)

		fsPath, err := resolvePath(rootDir, urlPath)
		if err != nil {
			http.Error(w, "Not found", http.StatusNotFound)
			return
		}

		info, err := os.Stat(fsPath)
		if err != nil {
			http.Error(w, "Not found", http.StatusNotFound)
			return
		}

		renderPreviewPage(w, r, siteName, defaultTheme, tmpl, urlPath, fsPath, info)
	}
}

// renderPreviewPage renders one already-resolved preview request using the
// caller-supplied file info so shared routing code can avoid duplicate Stat
// calls before delegating here.
//
// Parameters:
//   - w: the HTTP response writer that should receive the preview page or error.
//   - r: the current HTTP request whose user context and URL path shape the preview.
//   - siteName: the branding label shown in page titles and breadcrumbs.
//   - defaultTheme: the HTML theme class applied to the page root.
//   - tmpl: any renderer capable of executing the preview template.
//   - urlPath: the cleaned request URL path for the preview target.
//   - fsPath: the resolved filesystem path for the preview target.
//   - info: the already-loaded filesystem metadata for fsPath.
//
// Returns:
//   - none: the function writes the rendered page or an HTTP error directly to w.
func renderPreviewPage(w http.ResponseWriter, r *http.Request, siteName, defaultTheme string, tmpl interface {
	ExecutePreview(http.ResponseWriter, *models.PreviewData) error
}, urlPath, fsPath string, info os.FileInfo) {
	breadcrumbs := buildBreadcrumbs(siteName, path.Dir(urlPath))
	if !info.IsDir() {
		breadcrumbs = buildFileBreadcrumbs(siteName, urlPath)
	}

	previewData := &models.PreviewData{
		Title:        filepath.Base(fsPath),
		SiteName:     siteName,
		CurrentUser:  currentUserViewFromRequest(r),
		ShowSearch:   true,
		DefaultTheme: defaultTheme,
		FilePath:     urlPath,
		FileName:     filepath.Base(fsPath),
		Breadcrumbs:  breadcrumbs,
		ModTime:      info.ModTime(),
	}

	if info.IsDir() {
		previewData.IsDir = true
		if entries, err := os.ReadDir(fsPath); err == nil {
			previewData.EntryCount = len(entries)
		}
	} else {
		mimeType := mimeForFile(fsPath)
		previewData.MIMEType = mimeType
		previewData.ViewURL = "/view" + urlPath
		previewData.CanEdit = isEditableNoteFile(urlPath, mimeType)

		switch {
		case isImage(mimeType):
			previewData.IsImage = true

		case isPDF(mimeType):
			previewData.IsPDF = true

		case isText(mimeType):
			previewData.IsText = true

			content, err := readTextFile(fsPath)
			if err != nil {
				http.Error(w, "Could not read file", http.StatusInternalServerError)
				return
			}

			if isRenderable(mimeType) {
				previewData.RenderMode = "document"
				rendered, renderErr := renderContent(content, mimeType, urlPath)
				if renderErr != nil {
					previewData.RenderedContent = buildRenderStatusMessageHTML("render-preview-error", "Rendered preview is unavailable for this document right now.")
				} else {
					previewData.RenderedContent = rendered
				}
			} else {
				previewData.RenderMode = "codemirror"
				previewData.PlaintextContent = content
			}

		default:
			previewData.IsBinary = true
		}
	}

	if err := tmpl.ExecutePreview(w, previewData); err != nil {
		http.Error(w, "Template error", http.StatusInternalServerError)
	}
}

// HighlightCSSHandler serves the generated Chroma stylesheet for the active
// syntax-highlighting theme.
//
// Parameters:
//   - theme: the Chroma style name that should be converted into CSS.
//
// Returns:
//   - http.HandlerFunc: a handler that serves immutable CSS bytes from memory.
func HighlightCSSHandler(theme string) http.HandlerFunc {
	style := styles.Get(theme)
	if style == nil {
		style = styles.Fallback
	}
	formatter := chromahtml.New(chromahtml.WithClasses(true))
	var buf bytes.Buffer
	if err := formatter.WriteCSS(&buf, style); err != nil {
		buf.Reset()
	}
	css := buf.Bytes()

	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/css; charset=utf-8")
		_, _ = w.Write(css)
	}
}

// readTextFile loads a text file into memory with a hard size limit so a very
// large note cannot exhaust server memory during preview rendering.
//
// Parameters:
//   - fsPath: the filesystem path to the text file that should be read.
//
// Returns:
//   - string: the file contents truncated at the configured maximum read size.
//   - error: non-nil when the file cannot be opened or read.
func readTextFile(fsPath string) (string, error) {
	const maxBytes = 2 * 1024 * 1024
	file, err := os.Open(fsPath)
	if err != nil {
		return "", err
	}
	defer file.Close()

	data, err := io.ReadAll(io.LimitReader(file, maxBytes))
	if err != nil {
		return "", err
	}
	return string(data), nil
}
