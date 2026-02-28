// Package handlers contains HTTP handlers and supporting helpers used by Jotes.
package handlers

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

// MaxFileCreateBodySize is the maximum allowed size for file creation request bodies.
const MaxFileCreateBodySize = 64 * 1024 // 64KB

// MaxManagedFileUploadBodySize is the largest multipart upload request body
// that the managed upload endpoint will accept from the browser.
const MaxManagedFileUploadBodySize int64 = 2 << 30 // 2 GiB

// FileCreateRequest represents the JSON body for creating a file or directory.
type FileCreateRequest struct {
	Name    string `json:"name"`
	Type    string `json:"type"` // "file" or "directory"
	Content string `json:"content,omitempty"`
	Parent  string `json:"parent,omitempty"` // URL path of parent directory
}

// FileMoveRequest represents the JSON body for moving or renaming files.
type FileMoveRequest struct {
	Sources     []string `json:"sources"`              // Array of source paths (URL format)
	Destination string   `json:"destination"`          // Destination directory path (URL format)
	TargetName  string   `json:"targetName,omitempty"` // Optional replacement basename used only when exactly one source is supplied.
}

// FileDeleteRequest represents the JSON body for deleting files.
type FileDeleteRequest struct {
	Paths []string `json:"paths"` // Array of file/directory paths (URL format)
}

// FileCreateResponse represents the JSON response for file creation operations.
type FileCreateResponse struct {
	Success bool   `json:"success"`
	Path    string `json:"path,omitempty"`
	Error   string `json:"error,omitempty"`
}

// FileUploadResponse represents the JSON response for one uploaded file.
type FileUploadResponse struct {
	Success      bool   `json:"success"`
	Path         string `json:"path,omitempty"`
	BytesWritten int64  `json:"bytesWritten,omitempty"`
	Error        string `json:"error,omitempty"`
}

// FileMoveResponse represents the JSON response for file move operations.
type FileMoveResponse struct {
	Success bool     `json:"success"`
	Moved   []string `json:"moved,omitempty"`  // Array of successfully moved paths
	Failed  []string `json:"failed,omitempty"` // Array of failed moves with reasons
	Error   string   `json:"error,omitempty"`
}

// FileDeleteResponse represents the JSON response for file delete operations.
type FileDeleteResponse struct {
	Success bool     `json:"success"`
	Deleted []string `json:"deleted,omitempty"` // Array of successfully deleted paths
	Failed  []string `json:"failed,omitempty"`  // Array of failed deletions with reasons
	Error   string   `json:"error,omitempty"`
}

// DirectoryListResponse represents the JSON response for listing available directories.
type DirectoryListResponse struct {
	Success     bool            `json:"success"`
	Directories []DirectoryInfo `json:"directories,omitempty"`
	Warning     string          `json:"warning,omitempty"`
	Error       string          `json:"error,omitempty"`
}

// DirectoryInfo contains information about a single directory.
type DirectoryInfo struct {
	Path     string `json:"path"`     // URL path of the directory
	Name     string `json:"name"`     // Directory name (basename)
	Depth    int    `json:"depth"`    // Depth from root (0 = root, 1 = direct child, and so on)
	Relative string `json:"relative"` // Path relative to the caller-supplied "from" directory, or root-relative when "from" is "/"
}

type managedSourceEntry struct {
	URLPath       string
	FSPath        string
	Name          string
	IsDir         bool
	ParentURLPath string
}

type managedNoteCompanionPlan struct {
	SourceURL         string
	SourceFS          string
	TargetURL         string
	TargetFS          string
	SourceExists      bool
	ShouldMove        bool
	ShouldRewriteNote bool
	OldRelativePrefix string
	NewRelativePrefix string
}

type managedMovePlan struct {
	Source    managedSourceEntry
	TargetURL string
	TargetFS  string
	Companion *managedNoteCompanionPlan
}

// CreateFileHandler handles POST requests to create new files or directories.
//
// Parameters:
//   - rootDir: the filesystem root directory that backs the UI.
//
// Returns:
//   - http.HandlerFunc: a handler that creates files/directories on the filesystem.
func CreateFileHandler(rootDir string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			sendJSONError(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var req FileCreateRequest
		dec := json.NewDecoder(io.LimitReader(r.Body, MaxFileCreateBodySize))
		if err := dec.Decode(&req); err != nil {
			sendJSONError(w, "Invalid JSON request", http.StatusBadRequest)
			return
		}

		fileType := normalizeManagedCreateType(req.Type)
		entryName, err := inferManagedCreateEntryName(req.Name, fileType)
		if err != nil {
			sendJSONError(w, err.Error(), http.StatusBadRequest)
			return
		}

		parentURLPath := normalizeManagedURLPath(req.Parent)
		if pathContainsJotesCompanionDirectory(parentURLPath) {
			sendJSONError(w, "Managed .jotes companion folders cannot be modified directly", http.StatusBadRequest)
			return
		}
		parentFSPath, err := resolveManagedNonSymlinkPath(rootDir, parentURLPath)
		if err != nil {
			sendJSONError(w, "Invalid parent directory", http.StatusNotFound)
			return
		}

		parentInfo, err := os.Stat(parentFSPath)
		if err != nil || !parentInfo.IsDir() {
			sendJSONError(w, "Parent directory does not exist", http.StatusNotFound)
			return
		}

		newURLPath := path.Join(parentURLPath, entryName)
		newFSPath := filepath.Join(parentFSPath, entryName)
		if _, err := os.Lstat(newFSPath); err == nil {
			sendJSONError(w, "File or directory already exists", http.StatusConflict)
			return
		} else if !os.IsNotExist(err) {
			sendJSONError(w, "Could not validate the requested name", http.StatusInternalServerError)
			return
		}

		createdPath := newURLPath
		if fileType == "directory" {
			if err := os.Mkdir(newFSPath, 0o755); err != nil {
				sendJSONError(w, "Failed to create directory: "+err.Error(), http.StatusInternalServerError)
				return
			}
		} else {
			fileHandle, err := os.OpenFile(newFSPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
			if err != nil {
				sendJSONError(w, "Failed to create file: "+err.Error(), http.StatusInternalServerError)
				return
			}
			if _, err := fileHandle.WriteString(req.Content); err != nil {
				fileHandle.Close()
				_ = os.Remove(newFSPath)
				sendJSONError(w, "Failed to create file: "+err.Error(), http.StatusInternalServerError)
				return
			}
			if err := fileHandle.Close(); err != nil {
				_ = os.Remove(newFSPath)
				sendJSONError(w, "Failed to finalize file creation: "+err.Error(), http.StatusInternalServerError)
				return
			}
		}

		invalidateDirectoryListings()
		sendJSON(w, http.StatusCreated, FileCreateResponse{Success: true, Path: createdPath})
	}
}

// UploadFileHandler handles multipart POST requests that upload one browser
// selected file into the current managed directory without rewriting the
// browser-supplied basename.
//
// Parameters:
//   - rootDir: the filesystem root directory that bounds all managed uploads.
//
// Returns:
//   - http.HandlerFunc: a handler that stores one uploaded file beneath the requested parent directory.
func UploadFileHandler(rootDir string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			sendJSONError(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		r.Body = http.MaxBytesReader(w, r.Body, MaxManagedFileUploadBodySize)
		if err := r.ParseMultipartForm(32 << 20); err != nil {
			var maxBytesError *http.MaxBytesError
			if errors.As(err, &maxBytesError) {
				sendJSONError(w, "Uploaded file is too large", http.StatusRequestEntityTooLarge)
				return
			}
			sendJSONError(w, "Invalid upload request", http.StatusBadRequest)
			return
		}
		if r.MultipartForm != nil {
			defer r.MultipartForm.RemoveAll()
		}

		parentURLPath := normalizeManagedURLPath(r.FormValue("parent"))
		if pathContainsJotesCompanionDirectory(parentURLPath) {
			sendJSONError(w, "Managed .jotes companion folders cannot be modified directly", http.StatusBadRequest)
			return
		}
		parentFSPath, err := resolveManagedNonSymlinkPath(rootDir, parentURLPath)
		if err != nil {
			sendJSONError(w, "Invalid parent directory", http.StatusNotFound)
			return
		}

		parentInfo, err := os.Stat(parentFSPath)
		if err != nil || !parentInfo.IsDir() {
			sendJSONError(w, "Parent directory does not exist", http.StatusNotFound)
			return
		}

		uploadedFile, uploadedHeader, err := r.FormFile("file")
		if err != nil {
			sendJSONError(w, "Choose a file to upload", http.StatusBadRequest)
			return
		}
		defer uploadedFile.Close()

		entryName, err := normalizeManagedUploadEntryName(uploadedHeader.Filename)
		if err != nil {
			sendJSONError(w, err.Error(), http.StatusBadRequest)
			return
		}

		newURLPath := path.Join(parentURLPath, entryName)
		newFSPath := filepath.Join(parentFSPath, entryName)
		if _, err := os.Lstat(newFSPath); err == nil {
			sendJSONError(w, "File or directory already exists", http.StatusConflict)
			return
		} else if !os.IsNotExist(err) {
			sendJSONError(w, "Could not validate the requested name", http.StatusInternalServerError)
			return
		}

		bytesWritten, err := writeManagedUploadedFile(newFSPath, uploadedFile)
		if err != nil {
			sendJSONError(w, "Failed to store uploaded file: "+err.Error(), http.StatusInternalServerError)
			return
		}

		invalidateDirectoryListings()
		sendJSON(w, http.StatusCreated, FileUploadResponse{Success: true, Path: newURLPath, BytesWritten: bytesWritten})
	}
}

// normalizeManagedUploadEntryName validates the browser-supplied upload
// filename exactly as received so uploaded files keep their original basename
// without the Markdown-extension inference used by note creation.
//
// Parameters:
//   - uploadedName: the multipart filename reported by the browser for the selected file.
//
// Returns:
//   - string: the trimmed validated basename that should be written into the managed directory.
//   - error: non-nil when the uploaded filename is empty or violates the shared basename safety rules.
func normalizeManagedUploadEntryName(uploadedName string) (string, error) {
	normalizedName := strings.TrimSpace(uploadedName)
	if err := validateManagedBaseName(normalizedName); err != nil {
		return "", err
	}
	return normalizedName, nil
}

// writeManagedUploadedFile streams one uploaded file into a brand-new managed
// path, cleaning up any partially written destination when the write or close
// step fails.
//
// Parameters:
//   - destinationFSPath: the absolute filesystem path that should receive the uploaded file.
//   - contentReader: the multipart file reader that should be copied into destinationFSPath.
//
// Returns:
//   - int64: the exact number of bytes copied into the destination file.
//   - error: non-nil when the destination cannot be created, copied, or finalized safely.
func writeManagedUploadedFile(destinationFSPath string, contentReader io.Reader) (int64, error) {
	fileHandle, err := os.OpenFile(destinationFSPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return 0, err
	}

	bytesWritten, copyErr := io.Copy(fileHandle, contentReader)
	closeErr := fileHandle.Close()
	if copyErr != nil {
		_ = os.Remove(destinationFSPath)
		return 0, copyErr
	}
	if closeErr != nil {
		_ = os.Remove(destinationFSPath)
		return 0, closeErr
	}

	return bytesWritten, nil
}

// DeleteFileHandler handles POST requests to remove files or directories.
// Accepts JSON body with array of paths for bulk deletion.
//
// Parameters:
//   - rootDir: the filesystem root directory that backs the UI.
//
// Returns:
//   - http.HandlerFunc: a handler that deletes files/directories from the filesystem.
func DeleteFileHandler(rootDir string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			sendJSONError(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var req FileDeleteRequest
		dec := json.NewDecoder(io.LimitReader(r.Body, MaxFileCreateBodySize))
		if err := dec.Decode(&req); err != nil {
			sendJSONError(w, "Invalid JSON request", http.StatusBadRequest)
			return
		}

		paths := pruneNestedManagedURLPaths(uniqueNormalizedManagedURLPaths(req.Paths))
		if len(paths) == 0 {
			sendJSONError(w, "No paths specified for deletion", http.StatusBadRequest)
			return
		}

		deleted := make([]string, 0, len(paths))
		failed := make([]string, 0)

		for _, urlPath := range paths {
			if urlPath == "/" {
				failed = append(failed, "Cannot delete the configured root directory.")
				continue
			}
			if pathContainsJotesCompanionDirectory(urlPath) {
				failed = append(failed, "Managed .jotes companion folders cannot be deleted directly: "+urlPath)
				continue
			}

			fsPath, err := resolveManagedNonSymlinkPath(rootDir, urlPath)
			if err != nil {
				failed = append(failed, "Cannot resolve path: "+urlPath)
				continue
			}

			if _, err := os.Stat(fsPath); os.IsNotExist(err) {
				failed = append(failed, "Does not exist: "+urlPath)
				continue
			} else if err != nil {
				failed = append(failed, "Could not inspect "+urlPath+": "+err.Error())
				continue
			}

			if err := os.RemoveAll(fsPath); err != nil {
				failed = append(failed, "Failed to delete "+urlPath+": "+err.Error())
				continue
			}

			deleted = append(deleted, urlPath)
		}

		if len(deleted) > 0 {
			invalidateDirectoryListings()
		}

		sendJSON(w, http.StatusOK, FileDeleteResponse{
			Success: len(deleted) > 0,
			Deleted: deleted,
			Failed:  failed,
		})
	}
}

// MoveFileHandler handles POST requests to move or rename files/directories.
// Uses a destination directory that has already been selected by the browser.
//
// Parameters:
//   - rootDir: the filesystem root directory that backs the UI.
//
// Returns:
//   - http.HandlerFunc: a handler that moves/renames files on the filesystem.
func MoveFileHandler(rootDir string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			sendJSONError(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var req FileMoveRequest
		dec := json.NewDecoder(io.LimitReader(r.Body, MaxFileCreateBodySize))
		if err := dec.Decode(&req); err != nil {
			sendJSONError(w, "Invalid JSON request", http.StatusBadRequest)
			return
		}

		sourcePaths := uniqueNormalizedManagedURLPaths(req.Sources)
		if len(sourcePaths) == 0 {
			sendJSONError(w, "No source paths specified", http.StatusBadRequest)
			return
		}

		targetName, err := normalizeManagedMoveTargetName(req.TargetName)
		if err != nil {
			sendJSONError(w, err.Error(), http.StatusBadRequest)
			return
		}
		if targetName != "" && len(sourcePaths) != 1 {
			sendJSONError(w, "Renaming requires exactly one source path", http.StatusBadRequest)
			return
		}

		destURLPath := normalizeManagedURLPath(req.Destination)
		if pathContainsJotesCompanionDirectory(destURLPath) {
			sendJSONError(w, "Managed .jotes companion folders cannot be used as move destinations", http.StatusBadRequest)
			return
		}
		destFSPath, err := resolveManagedNonSymlinkPath(rootDir, destURLPath)
		if err != nil {
			sendJSONError(w, "Invalid destination directory", http.StatusNotFound)
			return
		}

		destInfo, err := os.Stat(destFSPath)
		if err != nil || !destInfo.IsDir() {
			sendJSONError(w, "Destination is not a valid directory", http.StatusNotFound)
			return
		}

		resolvedSources := make([]managedSourceEntry, 0, len(sourcePaths))
		failed := make([]string, 0)
		for _, sourceURLPath := range sourcePaths {
			sourceEntry, err := resolveManagedSourceEntry(rootDir, sourceURLPath)
			if err != nil {
				failed = append(failed, err.Error())
				continue
			}
			resolvedSources = append(resolvedSources, sourceEntry)
		}

		plans := make([]managedMovePlan, 0, len(resolvedSources))
		plannedTargets := make(map[string]string, len(resolvedSources))
		for _, sourceEntry := range resolvedSources {
			var companionPlan *managedNoteCompanionPlan

			effectiveTargetName := sourceEntry.Name
			if targetName != "" {
				effectiveTargetName = targetName
			}

			if moveFailure := validateManagedMoveSourceSelection(sourceEntry.URLPath, sourcePaths); moveFailure != "" {
				failed = append(failed, moveFailure)
				continue
			}
			if moveFailure := validateManagedMoveSourceCompanionSelection(sourceEntry); moveFailure != "" {
				failed = append(failed, moveFailure)
				continue
			}

			if moveFailure := validateManagedMoveDestination(sourceEntry, destURLPath, destFSPath, effectiveTargetName); moveFailure != "" {
				failed = append(failed, moveFailure)
				continue
			}

			targetFSPath := filepath.Join(destFSPath, effectiveTargetName)
			if existingSourceURLPath, exists := plannedTargets[targetFSPath]; exists {
				failed = append(failed, fmt.Sprintf("Cannot move %s because it would collide with %s in the destination directory.", sourceEntry.URLPath, existingSourceURLPath))
				continue
			}
			if _, err := os.Lstat(targetFSPath); err == nil {
				failed = append(failed, "Destination already exists: "+path.Join(destURLPath, effectiveTargetName))
				continue
			} else if !os.IsNotExist(err) {
				failed = append(failed, "Could not inspect destination for "+sourceEntry.URLPath+": "+err.Error())
				continue
			}

			targetURLPath := path.Join(destURLPath, effectiveTargetName)
			companionPlan, moveFailure := planManagedNoteCompanionMove(rootDir, sourceEntry, targetURLPath)
			if moveFailure != "" {
				failed = append(failed, moveFailure)
				continue
			}

			plannedTargets[targetFSPath] = sourceEntry.URLPath
			plans = append(plans, managedMovePlan{Source: sourceEntry, TargetURL: targetURLPath, TargetFS: targetFSPath, Companion: companionPlan})
		}

		moved := make([]string, 0, len(plans))
		for _, plan := range plans {
			if err := executeManagedMovePlan(rootDir, plan); err != nil {
				failed = append(failed, "Failed to move "+plan.Source.URLPath+": "+err.Error())
				continue
			}
			moved = append(moved, plan.Source.URLPath+" -> "+plan.TargetURL)
		}

		if len(moved) > 0 {
			invalidateDirectoryListings()
		}

		sendJSON(w, http.StatusOK, FileMoveResponse{
			Success: len(moved) > 0,
			Moved:   moved,
			Failed:  failed,
		})
	}
}

// ListDirectoriesHandler returns all directories in the watched root using a
// ripgrep-backed search over the in-scope directory inventory.
//
// Parameters:
//   - rootDir: the filesystem root directory that backs the UI.
//
// Returns:
//   - http.HandlerFunc: a handler that returns a JSON list of available directories.
func ListDirectoriesHandler(rootDir string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			sendJSONError(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		query := strings.TrimSpace(r.URL.Query().Get("q"))
		if err := validateManagedDirectorySearchQuery(query); err != nil {
			sendJSONError(w, err.Error(), http.StatusBadRequest)
			return
		}

		fromURLPath := normalizeManagedURLPath(r.URL.Query().Get("from"))
		fromFSPath, err := resolveManagedNonSymlinkPath(rootDir, fromURLPath)
		if err != nil {
			sendJSONError(w, "Invalid source directory", http.StatusBadRequest)
			return
		}
		fromInfo, err := os.Stat(fromFSPath)
		if err != nil || !fromInfo.IsDir() {
			sendJSONError(w, "Invalid source directory", http.StatusBadRequest)
			return
		}

		relativeDirectoryPaths, err := collectManagedRelativeDirectoryPaths(rootDir)
		if err != nil {
			sendJSONError(w, "Could not list directories", http.StatusInternalServerError)
			return
		}

		filteredDirectoryPaths, warning, err := filterManagedDirectoryPathsWithRipgrep(query, relativeDirectoryPaths)
		if err != nil {
			log.Printf("directory-list: ripgrep error: %v", err)
			sendJSONError(w, "Directory search is temporarily unavailable", http.StatusInternalServerError)
			return
		}

		fromRelativePath := normalizeRelativeSearchPath(strings.TrimPrefix(fromURLPath, "/"))
		directories := make([]DirectoryInfo, 0, len(filteredDirectoryPaths)+1)
		if managedRootDirectoryMatchesQuery(query) {
			directories = append(directories, buildManagedDirectoryInfo("", fromRelativePath))
		}
		for _, relativeDirectoryPath := range filteredDirectoryPaths {
			directories = append(directories, buildManagedDirectoryInfo(relativeDirectoryPath, fromRelativePath))
		}

		sortManagedDirectories(directories)
		sendJSON(w, http.StatusOK, DirectoryListResponse{Success: true, Directories: directories, Warning: warning})
	}
}

// normalizeManagedCreateType converts one raw create-request type into the
// supported "file" or "directory" values that the handlers understand.
//
// Parameters:
//   - requestedType: the raw JSON value supplied by the browser.
//
// Returns:
//   - string: "directory" when the caller explicitly requested it, otherwise "file".
func normalizeManagedCreateType(requestedType string) string {
	if strings.EqualFold(strings.TrimSpace(requestedType), "directory") {
		return "directory"
	}
	return "file"
}

// inferManagedCreateEntryName validates one browser-supplied basename and adds
// a default Markdown extension when the caller is creating a file without any
// extension.
//
// Parameters:
//   - requestedName: the raw basename entered by the user in the directory UI.
//   - fileType: the normalized create type returned by normalizeManagedCreateType.
//
// Returns:
//   - string: the validated basename, with ".md" appended for extensionless files.
//   - error: non-nil when the basename is empty, unsafe, or otherwise invalid for managed creation.
func inferManagedCreateEntryName(requestedName, fileType string) (string, error) {
	normalizedName := strings.TrimSpace(requestedName)
	if err := validateManagedBaseName(normalizedName); err != nil {
		return "", err
	}
	if fileType != "file" {
		return normalizedName, nil
	}

	extension := filepath.Ext(normalizedName)
	if extension == "" || extension == "." {
		return normalizedName + ".md", nil
	}
	return normalizedName, nil
}

// normalizeManagedMoveTargetName validates one optional rename target supplied
// to the move API, returning an empty string when the caller did not request a
// rename at all.
//
// Parameters:
//   - requestedName: the optional basename that should replace the moved entry's current name.
//
// Returns:
//   - string: the trimmed validated basename, or an empty string when requestedName is blank.
//   - error: non-nil when requestedName is non-empty but fails the shared basename validation rules.
func normalizeManagedMoveTargetName(requestedName string) (string, error) {
	normalizedName := strings.TrimSpace(requestedName)
	if normalizedName == "" {
		return "", nil
	}
	if err := validateManagedBaseName(normalizedName); err != nil {
		return "", err
	}
	return normalizedName, nil
}

// validateManagedBaseName enforces the basename-only naming rules used by the
// create API so the browser cannot smuggle traversal characters, reserved
// managed-folder names, or malformed path data into filesystem operations.
//
// Parameters:
//   - name: the user-supplied basename that should represent one file or directory name only.
//
// Returns:
//   - error: non-nil when name is empty, reserved, too long, or contains path separators or NUL bytes.
func validateManagedBaseName(name string) error {
	if name == "" {
		return fmt.Errorf("Name is required")
	}
	if name == "." || name == ".." {
		return fmt.Errorf("Invalid name")
	}
	if isJotesCompanionDirectoryName(name) {
		return fmt.Errorf("Names beginning with .jotes- are reserved for note image folders")
	}
	if len(name) > 255 {
		return fmt.Errorf("Name too long (max 255 characters)")
	}
	if strings.ContainsAny(name, "/\\?#%") {
		return fmt.Errorf("Invalid name: cannot contain path separators, %%, ? or #")
	}
	if strings.ContainsRune(name, '\x00') {
		return fmt.Errorf("Invalid name")
	}
	return nil
}

// normalizeManagedURLPath cleans one browser-supplied URL path and guarantees
// the returned value starts with a single leading slash inside the Jotes path
// model.
//
// Parameters:
//   - rawPath: the path fragment or full URL-style path supplied by the browser.
//
// Returns:
//   - string: the cleaned absolute-style URL path, defaulting to "/" when rawPath is empty.
func normalizeManagedURLPath(rawPath string) string {
	trimmedPath := strings.TrimSpace(rawPath)
	if trimmedPath == "" {
		return "/"
	}
	return path.Clean("/" + strings.TrimPrefix(trimmedPath, "/"))
}

// resolveManagedNonSymlinkPath converts one managed URL path into a filesystem
// path beneath the configured root while rejecting any request path that uses
// a symlinked component as an alias.
//
// Parameters:
//   - rootDir: the configured filesystem root that bounds all managed operations.
//   - urlPath: the cleaned Jotes URL path that should be resolved.
//
// Returns:
//   - string: the lexical filesystem path beneath rootDir that corresponds to urlPath.
//   - error: non-nil when urlPath escapes the root, cannot be resolved, or traverses any symlinked component.
func resolveManagedNonSymlinkPath(rootDir, urlPath string) (string, error) {
	if !strings.HasPrefix(urlPath, "/") {
		return "", fmt.Errorf("invalid path %q", urlPath)
	}

	resolvedRoot, err := filepath.EvalSymlinks(filepath.Clean(rootDir))
	if err != nil {
		return "", fmt.Errorf("resolve configured root %q: %w", rootDir, err)
	}

	relativePath := filepath.FromSlash(strings.TrimPrefix(urlPath, "/"))
	lexicalPath := filepath.Clean(filepath.Join(resolvedRoot, relativePath))
	if !pathWithinRoot(resolvedRoot, lexicalPath) {
		return "", fmt.Errorf("path traversal detected for %q", urlPath)
	}
	if relativePath == "" || relativePath == "." {
		return lexicalPath, nil
	}

	currentPath := resolvedRoot
	for _, segment := range strings.Split(relativePath, string(filepath.Separator)) {
		if segment == "" || segment == "." {
			continue
		}
		currentPath = filepath.Join(currentPath, segment)
		info, err := os.Lstat(currentPath)
		if os.IsNotExist(err) {
			return lexicalPath, nil
		}
		if err != nil {
			return "", fmt.Errorf("inspect %q: %w", urlPath, err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("path %q uses a symlink component", urlPath)
		}
	}

	return lexicalPath, nil
}

// uniqueNormalizedManagedURLPaths cleans, de-duplicates, and preserves the
// original order of one batch of browser-supplied URL paths.
//
// Parameters:
//   - rawPaths: the raw request paths supplied for bulk deletion or moving.
//
// Returns:
//   - []string: the cleaned unique URL paths in first-seen order, excluding blank inputs.
func uniqueNormalizedManagedURLPaths(rawPaths []string) []string {
	seenPaths := make(map[string]struct{}, len(rawPaths))
	normalizedPaths := make([]string, 0, len(rawPaths))

	for _, rawPath := range rawPaths {
		if strings.TrimSpace(rawPath) == "" {
			continue
		}

		normalizedPath := normalizeManagedURLPath(rawPath)
		if _, exists := seenPaths[normalizedPath]; exists {
			continue
		}
		seenPaths[normalizedPath] = struct{}{}
		normalizedPaths = append(normalizedPaths, normalizedPath)
	}

	return normalizedPaths
}

// pruneNestedManagedURLPaths removes redundant descendant paths when the same
// delete request already includes one of their ancestors, so recursive deletion
// executes once at the highest selected path.
//
// Parameters:
//   - urlPaths: the cleaned unique URL paths requested for deletion.
//
// Returns:
//   - []string: the subset of urlPaths that are not descendants of any earlier retained path.
func pruneNestedManagedURLPaths(urlPaths []string) []string {
	sortedPaths := append([]string(nil), urlPaths...)
	sort.SliceStable(sortedPaths, func(i, j int) bool {
		if len(sortedPaths[i]) != len(sortedPaths[j]) {
			return len(sortedPaths[i]) < len(sortedPaths[j])
		}
		return sortedPaths[i] < sortedPaths[j]
	})

	prunedPaths := make([]string, 0, len(sortedPaths))
	for _, candidatePath := range sortedPaths {
		shouldKeep := true
		for _, keptPath := range prunedPaths {
			if candidatePath == keptPath || strings.HasPrefix(candidatePath, keptPath+"/") {
				shouldKeep = false
				break
			}
		}
		if shouldKeep {
			prunedPaths = append(prunedPaths, candidatePath)
		}
	}

	return prunedPaths
}

// resolveManagedSourceEntry resolves one requested move source into validated
// filesystem metadata that later validation and move planning steps can reuse.
//
// Parameters:
//   - rootDir: the filesystem root directory that bounds every managed operation.
//   - sourceURLPath: the cleaned URL path that should be moved.
//
// Returns:
//   - managedSourceEntry: the resolved source description, including parent URL path and directory/file classification.
//   - error: non-nil when the source is the configured root, cannot be resolved, or does not currently exist.
func resolveManagedSourceEntry(rootDir, sourceURLPath string) (managedSourceEntry, error) {
	if sourceURLPath == "/" {
		return managedSourceEntry{}, fmt.Errorf("Cannot move the configured root directory.")
	}

	resolvedFSPath, err := resolveManagedNonSymlinkPath(rootDir, sourceURLPath)
	if err != nil {
		return managedSourceEntry{}, fmt.Errorf("Cannot resolve source: %s", sourceURLPath)
	}

	sourceInfo, err := os.Stat(resolvedFSPath)
	if os.IsNotExist(err) {
		return managedSourceEntry{}, fmt.Errorf("Source does not exist: %s", sourceURLPath)
	}
	if err != nil {
		return managedSourceEntry{}, fmt.Errorf("Could not inspect source %s: %v", sourceURLPath, err)
	}

	return managedSourceEntry{
		URLPath:       sourceURLPath,
		FSPath:        resolvedFSPath,
		Name:          path.Base(sourceURLPath),
		IsDir:         sourceInfo.IsDir(),
		ParentURLPath: path.Dir(sourceURLPath),
	}, nil
}

// validateManagedMoveSourceSelection rejects overlapping move requests where a
// later selected source is already contained by another selected source, which
// would otherwise cause double-processing or disappearing descendants.
//
// Parameters:
//   - sourceURLPath: the specific source path currently being validated.
//   - allSourceURLPaths: the full normalized move-source batch supplied by the browser.
//
// Returns:
//   - string: an empty string when the selection is valid, otherwise a user-facing failure message.
func validateManagedMoveSourceSelection(sourceURLPath string, allSourceURLPaths []string) string {
	for _, candidateAncestor := range allSourceURLPaths {
		if candidateAncestor == sourceURLPath {
			continue
		}
		if strings.HasPrefix(sourceURLPath, candidateAncestor+"/") {
			return fmt.Sprintf("Cannot move %s because %s is already selected and contains it.", sourceURLPath, candidateAncestor)
		}
	}
	return ""
}

// validateManagedMoveDestination applies destination safety rules for one move
// source, including no-op moves back into the same parent without renaming and
// directory moves into their own descendants.
//
// Parameters:
//   - sourceEntry: the already resolved file or directory being moved.
//   - destinationURLPath: the cleaned destination directory URL path chosen by the browser.
//   - destinationFSPath: the resolved filesystem directory that destinationURLPath points to.
//   - targetName: the basename that the source will have after the move completes.
//
// Returns:
//   - string: an empty string when the move is safe to plan, otherwise a user-facing failure message.
func validateManagedMoveDestination(sourceEntry managedSourceEntry, destinationURLPath, destinationFSPath, targetName string) string {
	if sourceEntry.ParentURLPath == destinationURLPath && sourceEntry.Name == targetName {
		return fmt.Sprintf("%s is already named %s in %s.", sourceEntry.URLPath, targetName, destinationURLPath)
	}
	if sourceEntry.IsDir && (sourceEntry.FSPath == destinationFSPath || pathWithinRoot(sourceEntry.FSPath, destinationFSPath)) {
		return fmt.Sprintf("Cannot move %s into itself or one of its subdirectories.", sourceEntry.URLPath)
	}
	return ""
}

// validateManagedMoveSourceCompanionSelection rejects direct moves and renames
// of managed .jotes companion folders or anything stored inside them so those
// paths stay coupled to their owning note.
//
// Parameters:
//   - sourceEntry: the already resolved move source currently being validated.
//
// Returns:
//   - string: an empty string when sourceEntry can be moved directly, otherwise a user-facing failure message.
func validateManagedMoveSourceCompanionSelection(sourceEntry managedSourceEntry) string {
	if !pathContainsJotesCompanionDirectory(sourceEntry.URLPath) {
		return ""
	}

	return fmt.Sprintf("Managed .jotes companion folders move with their note and cannot be moved directly: %s", sourceEntry.URLPath)
}

// managedDirectoryExistsAtPath reports whether one filesystem path already
// exists as a real directory while rejecting non-directory collisions and
// symlink aliases in the reserved managed companion-folder namespace.
//
// Parameters:
//   - fsPath: the filesystem path that should be inspected.
//
// Returns:
//   - bool: true when fsPath exists as a real directory, otherwise false.
//   - error: non-nil when fsPath exists as a non-directory or symlink, or when it cannot be inspected reliably.
func managedDirectoryExistsAtPath(fsPath string) (bool, error) {
	info, err := os.Lstat(fsPath)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return false, fmt.Errorf("managed companion path %s cannot use a symlink", fsPath)
	}
	if !info.IsDir() {
		return false, fmt.Errorf("managed companion path %s already exists and is not a directory", fsPath)
	}
	return true, nil
}

// planManagedNoteCompanionMove prepares the optional companion-folder move and
// in-note reference rewrite required when one Markdown or Org note is moved or
// renamed through the managed file API.
//
// Parameters:
//   - rootDir: the filesystem root directory that bounds every managed note and companion folder.
//   - sourceEntry: the resolved move source currently being planned.
//   - targetURLPath: the absolute-style Jotes URL path that sourceEntry will have after the move completes.
//
// Returns:
//   - *managedNoteCompanionPlan: the companion-folder work that should accompany the note move, or nil when sourceEntry is not a Markdown or Org note.
//   - string: an empty string when planning succeeded, otherwise a user-facing failure message.
func planManagedNoteCompanionMove(rootDir string, sourceEntry managedSourceEntry, targetURLPath string) (*managedNoteCompanionPlan, string) {
	var sourceCompanionExists bool
	var err error

	if sourceEntry.IsDir || !supportsPastedEditorImageAssets(mimeForFile(sourceEntry.FSPath)) {
		return nil, ""
	}

	sourceCompanionURL := jotesCompanionDirectoryURLPath(sourceEntry.URLPath)
	sourceCompanionFS, err := resolveManagedNonSymlinkPath(rootDir, sourceCompanionURL)
	if err != nil {
		return nil, fmt.Sprintf("Could not resolve the managed note image folder for %s.", sourceEntry.URLPath)
	}
	sourceCompanionExists, err = managedDirectoryExistsAtPath(sourceCompanionFS)
	if err != nil {
		return nil, fmt.Sprintf("Could not inspect the managed note image folder for %s: %v", sourceEntry.URLPath, err)
	}

	targetCompanionURL := jotesCompanionDirectoryURLPath(targetURLPath)
	targetCompanionFS, err := resolveManagedNonSymlinkPath(rootDir, targetCompanionURL)
	if err != nil {
		return nil, fmt.Sprintf("Could not resolve the managed note image destination for %s.", targetURLPath)
	}
	if sourceCompanionExists && sourceCompanionURL != targetCompanionURL {
		if _, err := os.Lstat(targetCompanionFS); err == nil {
			return nil, "Destination already exists: " + targetCompanionURL
		} else if !os.IsNotExist(err) {
			return nil, fmt.Sprintf("Could not inspect the managed note image destination for %s: %v", targetURLPath, err)
		}
	}

	return &managedNoteCompanionPlan{
		SourceURL:         sourceCompanionURL,
		SourceFS:          sourceCompanionFS,
		TargetURL:         targetCompanionURL,
		TargetFS:          targetCompanionFS,
		SourceExists:      sourceCompanionExists,
		ShouldMove:        sourceCompanionExists && sourceCompanionURL != targetCompanionURL,
		ShouldRewriteNote: jotesCompanionRelativePathPrefix(sourceEntry.URLPath) != jotesCompanionRelativePathPrefix(targetURLPath),
		OldRelativePrefix: jotesCompanionRelativePathPrefix(sourceEntry.URLPath),
		NewRelativePrefix: jotesCompanionRelativePathPrefix(targetURLPath),
	}, ""
}

// rewriteManagedNoteCompanionReferences updates one moved or renamed Markdown
// or Org note so any pasted-image references keep pointing at the note's new
// managed .jotes companion-folder prefix.
//
// Parameters:
//   - rootDir: the filesystem root directory that bounds every managed note.
//   - noteURLPath: the absolute-style Jotes URL path of the note whose source text may need rewriting.
//   - oldRelativePrefix: the previous note-relative companion-folder prefix that should be replaced.
//   - newRelativePrefix: the next note-relative companion-folder prefix that should replace oldRelativePrefix.
//
// Returns:
//   - error: non-nil when the note could not be read or atomically rewritten after the move.
func rewriteManagedNoteCompanionReferences(rootDir, noteURLPath, oldRelativePrefix, newRelativePrefix string) error {
	var updatedContent string

	if oldRelativePrefix == newRelativePrefix {
		return nil
	}

	noteFSPath, err := resolveManagedNonSymlinkPath(rootDir, noteURLPath)
	if err != nil {
		return err
	}
	currentContentBytes, err := os.ReadFile(noteFSPath)
	if err != nil {
		return err
	}
	currentContent := string(currentContentBytes)
	updatedContent = strings.ReplaceAll(currentContent, oldRelativePrefix, newRelativePrefix)
	if updatedContent == currentContent {
		return nil
	}

	_, err = saveEditableTextFile(noteURLPath, rootDir, buildContentRevision(currentContent), updatedContent)
	return err
}

// rollbackManagedMovePlan attempts to restore a note move after a later
// companion-reference rewrite failed, returning the first rollback error that
// prevented a clean restore.
//
// Parameters:
//   - plan: the already executed move plan that should be reverted.
//   - noteMoved: true when the note file itself has already been renamed or moved to plan.TargetFS.
//   - companionMoved: true when the managed companion folder has already been renamed or moved to plan.Companion.TargetFS.
//
// Returns:
//   - error: non-nil when any rollback step fails, otherwise nil after the original paths are restored.
func rollbackManagedMovePlan(plan managedMovePlan, noteMoved, companionMoved bool) error {
	var rollbackErr error

	if noteMoved {
		if err := os.Rename(plan.TargetFS, plan.Source.FSPath); err != nil {
			rollbackErr = err
		}
	}
	if companionMoved && plan.Companion != nil {
		if err := os.Rename(plan.Companion.TargetFS, plan.Companion.SourceFS); err != nil && rollbackErr == nil {
			rollbackErr = err
		}
	}

	return rollbackErr
}

// executeManagedMovePlan performs one already validated move plan, including
// any companion-folder move and note-source rewrite required to keep pasted
// image links aligned with the note's new path.
//
// Parameters:
//   - rootDir: the filesystem root directory that bounds every managed note and companion folder.
//   - plan: the fully validated move plan that should be executed.
//
// Returns:
//   - error: non-nil when the move, companion-folder move, or note-source rewrite fails.
func executeManagedMovePlan(rootDir string, plan managedMovePlan) error {
	companionMoved := false
	noteMoved := false

	if plan.Companion != nil && plan.Companion.ShouldMove {
		if err := os.Rename(plan.Companion.SourceFS, plan.Companion.TargetFS); err != nil {
			return fmt.Errorf("failed to move managed note image folder %s: %w", plan.Companion.SourceURL, err)
		}
		companionMoved = true
	}

	if err := os.Rename(plan.Source.FSPath, plan.TargetFS); err != nil {
		if rollbackErr := rollbackManagedMovePlan(plan, noteMoved, companionMoved); rollbackErr != nil {
			return fmt.Errorf("failed to move %s: %v; rollback failed: %v", plan.Source.URLPath, err, rollbackErr)
		}
		return err
	}
	noteMoved = true

	if plan.Companion != nil && plan.Companion.ShouldRewriteNote {
		if err := rewriteManagedNoteCompanionReferences(rootDir, plan.TargetURL, plan.Companion.OldRelativePrefix, plan.Companion.NewRelativePrefix); err != nil {
			if rollbackErr := rollbackManagedMovePlan(plan, noteMoved, companionMoved); rollbackErr != nil {
				return fmt.Errorf("failed to rewrite managed note image references for %s: %v; rollback failed: %v", plan.TargetURL, err, rollbackErr)
			}
			return fmt.Errorf("failed to rewrite managed note image references for %s: %w", plan.TargetURL, err)
		}
	}

	return nil
}

// validateManagedDirectorySearchQuery enforces the lightweight input rules for
// ripgrep-backed directory search so the browser can pass literal directory
// fragments safely.
//
// Parameters:
//   - query: the raw directory search query string supplied by the browser.
//
// Returns:
//   - error: non-nil when query is too large or contains control characters that would break line-based searching.
func validateManagedDirectorySearchQuery(query string) error {
	if len(query) > 256 {
		return fmt.Errorf("Search query too long")
	}
	if strings.ContainsAny(query, "\x00\n\r") {
		return fmt.Errorf("Invalid search query")
	}
	return nil
}

// collectManagedRelativeDirectoryPaths walks the configured root directory and
// returns every in-scope directory path relative to that root, excluding the
// root itself and skipping symlinked entries.
//
// Parameters:
//   - rootDir: the absolute filesystem root that bounds Jotes content.
//
// Returns:
//   - []string: slash-delimited directory paths relative to rootDir.
//   - error: non-nil when the root cannot be walked at all.
func collectManagedRelativeDirectoryPaths(rootDir string) ([]string, error) {
	relativeDirectoryPaths := make([]string, 0, 64)
	walkErr := filepath.WalkDir(rootDir, func(currentPath string, entry fs.DirEntry, err error) error {
		if err != nil {
			if currentPath == rootDir {
				return err
			}
			log.Printf("directory-list: skipping unreadable path %q: %v", currentPath, err)
			return filepath.SkipDir
		}
		if currentPath == rootDir {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if !entry.IsDir() {
			return nil
		}
		if isJotesCompanionDirectoryName(entry.Name()) {
			return filepath.SkipDir
		}

		relativePath, err := filepath.Rel(rootDir, currentPath)
		if err != nil {
			log.Printf("directory-list: could not relativize %q: %v", currentPath, err)
			return filepath.SkipDir
		}

		normalizedRelativePath := normalizeRelativeSearchPath(relativePath)
		if normalizedRelativePath != "" {
			relativeDirectoryPaths = append(relativeDirectoryPaths, normalizedRelativePath)
		}
		return nil
	})
	if walkErr != nil {
		return nil, walkErr
	}

	return relativeDirectoryPaths, nil
}

// filterManagedDirectoryPathsWithRipgrep searches one already scoped directory
// inventory with ripgrep so the browser receives only matching directories
// without ever searching outside the configured root.
//
// Parameters:
//   - query: the literal directory fragment entered by the user; an empty query returns every directory unchanged.
//   - relativeDirectoryPaths: the complete in-scope directory inventory relative to the configured root.
//
// Returns:
//   - []string: the subset of relativeDirectoryPaths whose text matched the query.
//   - string: an optional warning when ripgrep returned only partial results.
//   - error: non-nil when ripgrep could not be started or completed reliably enough to serve the request.
func filterManagedDirectoryPathsWithRipgrep(query string, relativeDirectoryPaths []string) ([]string, string, error) {
	if query == "" {
		return relativeDirectoryPaths, "", nil
	}

	var stderr bytes.Buffer
	args := []string{"--json", "--ignore-case", "--fixed-strings", "--line-number", "--color", "never", "--", query, "-"}
	cmd := exec.Command("rg", args...)
	cmd.Stderr = &stderr
	cmd.Stdin = strings.NewReader(strings.Join(relativeDirectoryPaths, "\n"))

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, "", err
	}
	if err := cmd.Start(); err != nil {
		return nil, "", err
	}

	matches := make([]string, 0, 16)
	seenMatches := make(map[string]struct{}, 16)
	warning := ""
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 16*1024), 4*1024*1024)
	for scanner.Scan() {
		var message RipgrepJSONMessage
		if err := json.Unmarshal(scanner.Bytes(), &message); err != nil {
			log.Printf("directory-list: failed to parse ripgrep JSON: %v", err)
			warning = joinSearchWarnings(warning, "Directory search returned partial results.")
			continue
		}
		if message.Type != "match" {
			continue
		}

		normalizedRelativePath := normalizeRelativeSearchPath(message.Data.Lines.Text)
		if normalizedRelativePath == "" {
			continue
		}
		if _, exists := seenMatches[normalizedRelativePath]; exists {
			continue
		}
		seenMatches[normalizedRelativePath] = struct{}{}
		matches = append(matches, normalizedRelativePath)
	}
	if err := scanner.Err(); err != nil {
		log.Printf("directory-list: failed to read ripgrep output: %v", err)
		warning = joinSearchWarnings(warning, "Directory search returned partial results.")
	}

	if err := cmd.Wait(); err != nil {
		stderrText := normalizeRipgrepStderr(stderr.String())
		if exitError, ok := err.(*exec.ExitError); ok {
			switch exitError.ExitCode() {
			case 1:
				return matches, warning, nil
			default:
				if stderrText != "" {
					log.Printf("directory-list: ripgrep exited with code %d: %s", exitError.ExitCode(), stderrText)
				} else {
					log.Printf("directory-list: ripgrep exited with code %d", exitError.ExitCode())
				}
				if len(matches) > 0 {
					warning = joinSearchWarnings(warning, "Directory search returned partial results.")
					return matches, warning, nil
				}
				return nil, "", err
			}
		}
		return nil, "", err
	}

	return matches, warning, nil
}

// managedRootDirectoryMatchesQuery decides whether the configured root
// directory should be included alongside non-root directory results for the
// current search query.
//
// Parameters:
//   - query: the literal directory search query entered by the user.
//
// Returns:
//   - bool: true when the root directory should appear in the response, otherwise false.
func managedRootDirectoryMatchesQuery(query string) bool {
	normalizedQuery := strings.ToLower(strings.TrimSpace(query))
	if normalizedQuery == "" {
		return true
	}
	return normalizedQuery == "/" || normalizedQuery == "." || strings.Contains("root", normalizedQuery)
}

// buildManagedDirectoryInfo converts one relative directory path into the JSON
// shape expected by the move destination picker, including a display path that
// is relative to the caller-supplied current directory when possible.
//
// Parameters:
//   - relativeDirectoryPath: the slash-delimited directory path relative to the configured root; an empty string represents the root directory itself.
//   - fromRelativePath: the caller's current directory path relative to the configured root, without a leading slash.
//
// Returns:
//   - DirectoryInfo: the normalized directory metadata sent back to the browser.
func buildManagedDirectoryInfo(relativeDirectoryPath, fromRelativePath string) DirectoryInfo {
	normalizedRelativePath := normalizeRelativeSearchPath(relativeDirectoryPath)
	urlPath := "/"
	name := "(root)"
	depth := 0
	if normalizedRelativePath != "" {
		urlPath = "/" + normalizedRelativePath
		name = path.Base(normalizedRelativePath)
		depth = strings.Count(normalizedRelativePath, "/") + 1
	}

	relativeDisplay := "."
	if normalizedRelativePath != "" && fromRelativePath == "" {
		relativeDisplay = normalizedRelativePath
	} else if normalizedRelativePath == "" && fromRelativePath == "" {
		relativeDisplay = "."
	} else {
		fromFilesystemPath := "."
		if fromRelativePath != "" {
			fromFilesystemPath = filepath.FromSlash(fromRelativePath)
		}
		targetFilesystemPath := "."
		if normalizedRelativePath != "" {
			targetFilesystemPath = filepath.FromSlash(normalizedRelativePath)
		}
		if relPath, err := filepath.Rel(fromFilesystemPath, targetFilesystemPath); err == nil {
			relativeDisplay = filepath.ToSlash(relPath)
		} else if normalizedRelativePath != "" {
			relativeDisplay = normalizedRelativePath
		}
	}
	if relativeDisplay == "" {
		relativeDisplay = "."
	}

	return DirectoryInfo{Path: urlPath, Name: name, Depth: depth, Relative: relativeDisplay}
}

// sortManagedDirectories keeps directory search responses stable by sorting the
// root first, then shallower directories before deeper ones, and finally by
// their URL paths.
//
// Parameters:
//   - directories: the response slice that should be sorted in place.
//
// Returns:
//   - none: directories is reordered directly.
func sortManagedDirectories(directories []DirectoryInfo) {
	sort.Slice(directories, func(i, j int) bool {
		if directories[i].Path == "/" || directories[j].Path == "/" {
			return directories[i].Path == "/"
		}
		if directories[i].Depth != directories[j].Depth {
			return directories[i].Depth < directories[j].Depth
		}
		return directories[i].Path < directories[j].Path
	})
}

// sendJSON encodes a response as JSON with appropriate headers.
//
// Parameters:
//   - w: the HTTP response writer.
//   - status: the HTTP status code to return.
//   - data: the Go value to encode as JSON.
//
// Returns:
//   - none: writes directly to the response writer.
func sendJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		log.Printf("json encode error: %v", err)
	}
}

// APIErrorResponse is a generic error response used across all file management endpoints.
type APIErrorResponse struct {
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
}

// sendJSONError sends a JSON error response.
//
// Parameters:
//   - w: the HTTP response writer.
//   - message: the error message to include in the response.
//   - status: the HTTP status code to return.
//
// Returns:
//   - none: writes directly to the response writer.
func sendJSONError(w http.ResponseWriter, message string, status int) {
	sendJSON(w, status, APIErrorResponse{
		Success: false,
		Error:   message,
	})
}
