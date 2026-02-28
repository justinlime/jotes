// Package handlers contains HTTP handlers and supporting helpers used by Jotes.
package handlers

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
)

const maxEditorImageUploadBytes int64 = 64 << 20 // 64 MiB

type editorImageUploadResponse struct {
	Success      bool   `json:"success"`
	AssetPath    string `json:"assetPath,omitempty"`
	RelativePath string `json:"relativePath,omitempty"`
	BytesWritten int64  `json:"bytesWritten,omitempty"`
	MIMEType     string `json:"mimeType,omitempty"`
	Error        string `json:"error,omitempty"`
}

// UploadEditorImageHandler handles multipart pasted-image uploads from the
// Markdown and Org browser editor, storing each uploaded image inside the
// note's managed sibling companion directory.
//
// Parameters:
//   - rootDir: the filesystem root directory that bounds every editor-managed note and pasted image.
//
// Returns:
//   - http.HandlerFunc: a handler that validates the note, stores the pasted image, and returns the inserted relative asset path.
func UploadEditorImageHandler(rootDir string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			writeEditorJSON(w, http.StatusMethodNotAllowed, editorErrorResponse{Error: "Method not allowed"})
			return
		}

		if err := validateEditorMultipartWriteRequest(r); err != nil {
			writeEditorRequestError(w, err)
			return
		}

		r.Body = http.MaxBytesReader(w, r.Body, maxEditorImageUploadBytes)
		if err := r.ParseMultipartForm(32 << 20); err != nil {
			var maxBytesError *http.MaxBytesError
			if errors.As(err, &maxBytesError) {
				writeEditorJSON(w, http.StatusRequestEntityTooLarge, editorErrorResponse{Error: "Pasted image is too large for the browser editor"})
				return
			}
			writeEditorJSON(w, http.StatusBadRequest, editorErrorResponse{Error: "Invalid pasted image upload request"})
			return
		}
		if r.MultipartForm != nil {
			defer r.MultipartForm.RemoveAll()
		}

		noteURLPath, _, mimeType, err := resolveEditableFile(rootDir, r.FormValue("path"))
		if err != nil {
			writeEditorRequestError(w, err)
			return
		}
		if !supportsPastedEditorImageAssets(mimeType) {
			writeEditorJSON(w, http.StatusUnsupportedMediaType, editorErrorResponse{Error: "Pasted images are only supported in Markdown and Org notes"})
			return
		}
		if pathContainsJotesCompanionDirectory(noteURLPath) {
			writeEditorJSON(w, http.StatusBadRequest, editorErrorResponse{Error: "Managed pasted-image folders cannot be edited as source notes"})
			return
		}

		uploadedFile, uploadedHeader, err := r.FormFile("file")
		if err != nil {
			writeEditorJSON(w, http.StatusBadRequest, editorErrorResponse{Error: "Choose an image to paste into the current note"})
			return
		}
		defer uploadedFile.Close()

		bufferedFile := bufio.NewReader(uploadedFile)
		detectedMimeType, fileExtension, err := detectUploadedEditorImageType(bufferedFile, uploadedHeader.Header.Get("Content-Type"))
		if err != nil {
			writeEditorJSON(w, http.StatusUnsupportedMediaType, editorErrorResponse{Error: err.Error()})
			return
		}

		companionURLPath := jotesCompanionDirectoryURLPath(noteURLPath)
		companionFSPath, err := resolveManagedNonSymlinkPath(rootDir, companionURLPath)
		if err != nil {
			writeEditorJSON(w, http.StatusInternalServerError, editorErrorResponse{Error: "Jotes could not resolve the note image folder"})
			return
		}
		if err := ensureEditorImageCompanionDirectory(companionURLPath, companionFSPath); err != nil {
			writeEditorJSON(w, http.StatusInternalServerError, editorErrorResponse{Error: "Jotes could not prepare the note image folder"})
			return
		}

		assetFileName, assetURLPath, assetFSPath, err := nextAvailableEditorImageAssetPath(companionURLPath, companionFSPath, fileExtension, time.Now().UTC())
		if err != nil {
			writeEditorJSON(w, http.StatusInternalServerError, editorErrorResponse{Error: "Jotes could not prepare a filename for the pasted image"})
			return
		}

		bytesWritten, err := writeManagedUploadedFile(assetFSPath, bufferedFile)
		if err != nil {
			writeEditorJSON(w, http.StatusInternalServerError, editorErrorResponse{Error: "Failed to store pasted image: " + err.Error()})
			return
		}

		invalidateDirectoryListings()
		writeEditorJSON(w, http.StatusCreated, editorImageUploadResponse{
			Success:      true,
			AssetPath:    assetURLPath,
			RelativePath: buildNoteRelativeCompanionAssetPath(noteURLPath, assetFileName),
			BytesWritten: bytesWritten,
			MIMEType:     detectedMimeType,
		})
	}
}

// detectUploadedEditorImageType inspects one pasted-image upload and resolves
// the supported image MIME type plus filename extension that should be used for
// storage on disk, trusting only the server-side content sniff instead of the
// browser-declared multipart Content-Type.
//
// Parameters:
//   - bufferedFile: a buffered reader positioned at the start of the uploaded file so the function can sniff its first bytes without consuming them for the later file write.
//   - declaredMimeType: the browser-declared multipart content type for the uploaded file part, used only to tailor unsupported-image error messages.
//
// Returns:
//   - string: the normalized supported image MIME type chosen for the upload.
//   - string: the filesystem filename extension, including the leading dot, that matches the chosen MIME type.
//   - error: non-nil when the upload is not a supported pasted image type.
func detectUploadedEditorImageType(bufferedFile *bufio.Reader, declaredMimeType string) (string, string, error) {
	sniffedHeader, err := bufferedFile.Peek(512)
	if err != nil && !errors.Is(err, bufio.ErrBufferFull) && !errors.Is(err, io.EOF) {
		return "", "", err
	}

	sniffedMimeType := normalizeUploadedEditorImageMimeType(http.DetectContentType(sniffedHeader))
	normalizedDeclaredMimeType := normalizeUploadedEditorImageMimeType(declaredMimeType)

	if fileExtension, ok := editorImageFileExtensionForMime(sniffedMimeType); ok {
		return sniffedMimeType, fileExtension, nil
	}
	if strings.HasPrefix(sniffedMimeType, "image/") || strings.HasPrefix(normalizedDeclaredMimeType, "image/") {
		return "", "", errors.New("Jotes currently supports pasted PNG, JPEG, GIF, WebP, BMP, and TIFF images only")
	}

	return "", "", errors.New("Only pasted images can be uploaded from the editor")
}

// normalizeUploadedEditorImageMimeType reduces one raw MIME type string to its
// lowercase media-type token without any parameters.
//
// Parameters:
//   - rawMimeType: the raw MIME type string to normalize.
//
// Returns:
//   - string: the lowercase media-type token, or an empty string when rawMimeType is blank.
func normalizeUploadedEditorImageMimeType(rawMimeType string) string {
	return strings.ToLower(strings.TrimSpace(strings.Split(rawMimeType, ";")[0]))
}

// editorImageFileExtensionForMime maps one supported pasted-image MIME type to
// the filename extension that Jotes should use when storing the uploaded file
// on disk.
//
// Parameters:
//   - mimeType: the normalized image MIME type chosen for one uploaded pasted image.
//
// Returns:
//   - string: the filename extension, including its leading dot, for mimeType.
//   - bool: true when mimeType is supported for pasted-image uploads, otherwise false.
func editorImageFileExtensionForMime(mimeType string) (string, bool) {
	switch mimeType {
	case "image/png":
		return ".png", true
	case "image/jpeg":
		return ".jpg", true
	case "image/gif":
		return ".gif", true
	case "image/webp":
		return ".webp", true
	case "image/bmp":
		return ".bmp", true
	case "image/tiff":
		return ".tiff", true
	default:
		return "", false
	}
}

// ensureEditorImageCompanionDirectory creates the hidden sibling directory used
// for pasted images when it does not exist yet and verifies that any existing
// path at the reserved location is a real directory.
//
// Parameters:
//   - companionURLPath: the absolute-style Jotes URL path of the note's companion directory, used for user-facing error context.
//   - companionFSPath: the filesystem path that should contain the pasted images for the current note.
//
// Returns:
//   - error: non-nil when the companion directory cannot be created safely or the reserved path already exists as a non-directory.
func ensureEditorImageCompanionDirectory(companionURLPath, companionFSPath string) error {
	info, err := os.Lstat(companionFSPath)
	if err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("managed note image path %s cannot use a symlink", companionURLPath)
		}
		if !info.IsDir() {
			return fmt.Errorf("managed note image path %s already exists and is not a directory", companionURLPath)
		}
		return nil
	}
	if !os.IsNotExist(err) {
		return err
	}

	return os.Mkdir(companionFSPath, 0o755)
}

// buildEditorImageAssetBaseName generates the timestamped basename stem used
// for one stored pasted-image file before any collision suffix or extension is
// appended.
//
// Parameters:
//   - createdAt: the upload timestamp that should be embedded in the generated filename.
//
// Returns:
//   - string: the extensionless pasted-image basename stem.
func buildEditorImageAssetBaseName(createdAt time.Time) string {
	return createdAt.UTC().Format("pasted-image-20060102-150405")
}

// nextAvailableEditorImageAssetPath chooses the first unused pasted-image
// filename inside one companion directory and returns the matching URL and
// filesystem paths.
//
// Parameters:
//   - companionURLPath: the absolute-style Jotes URL path of the note's companion directory.
//   - companionFSPath: the filesystem path of the note's companion directory.
//   - fileExtension: the validated filename extension, including its leading dot, for the uploaded image type.
//   - createdAt: the timestamp used to generate the base pasted-image filename.
//
// Returns:
//   - string: the chosen pasted-image basename including its extension.
//   - string: the absolute-style Jotes URL path where the uploaded image will be stored.
//   - string: the filesystem path where the uploaded image will be stored.
//   - error: non-nil when the directory contents cannot be inspected safely while searching for a free filename.
func nextAvailableEditorImageAssetPath(companionURLPath, companionFSPath, fileExtension string, createdAt time.Time) (string, string, string, error) {
	baseName := buildEditorImageAssetBaseName(createdAt)
	attempt := 0

	for {
		attempt += 1
		assetFileName := baseName + fileExtension
		if attempt > 1 {
			assetFileName = fmt.Sprintf("%s-%d%s", baseName, attempt, fileExtension)
		}

		assetFSPath := filepath.Join(companionFSPath, assetFileName)
		if _, err := os.Lstat(assetFSPath); os.IsNotExist(err) {
			return assetFileName, path.Join(companionURLPath, assetFileName), assetFSPath, nil
		} else if err != nil {
			return "", "", "", err
		}
	}
}
