// Package handlers contains HTTP handlers and supporting helpers used by Jotes.
package handlers

import (
	"path"
	"strings"
)

const jotesCompanionDirectoryPrefix = ".jotes-"

// supportsPastedEditorImageAssets reports whether one editable note MIME type
// should accept pasted-image uploads into a managed sibling companion
// directory.
//
// Parameters:
//   - mimeType: the detected note MIME type whose pasted-image support should be checked.
//
// Returns:
//   - bool: true for Markdown and Org notes, otherwise false.
func supportsPastedEditorImageAssets(mimeType string) bool {
	switch baseMIME(mimeType) {
	case "text/markdown", "text/x-org":
		return true
	default:
		return false
	}
}

// jotesCompanionDirectoryName builds the reserved hidden companion-directory
// basename used to store pasted images for one note file.
//
// Parameters:
//   - noteName: the basename of the note file that owns the pasted images.
//
// Returns:
//   - string: the reserved sibling-directory basename in the form ".jotes-<note filename>".
func jotesCompanionDirectoryName(noteName string) string {
	return jotesCompanionDirectoryPrefix + strings.TrimSpace(noteName)
}

// jotesCompanionDirectoryURLPath builds the managed companion-directory URL
// path that should sit next to one note file beneath the same parent
// directory.
//
// Parameters:
//   - noteURLPath: the absolute-style Jotes URL path of the owning note file.
//
// Returns:
//   - string: the absolute-style URL path of the note's companion directory.
func jotesCompanionDirectoryURLPath(noteURLPath string) string {
	cleanNotePath := path.Clean("/" + strings.TrimPrefix(strings.TrimSpace(noteURLPath), "/"))
	return path.Join(path.Dir(cleanNotePath), jotesCompanionDirectoryName(path.Base(cleanNotePath)))
}

// buildNoteRelativeCompanionAssetPath builds the relative note-source path
// that should be inserted into Markdown or Org content for one pasted image.
//
// Parameters:
//   - noteURLPath: the absolute-style Jotes URL path of the owning note file.
//   - assetFileName: the basename of the uploaded image stored inside the companion directory.
//
// Returns:
//   - string: the relative path from the note file to the stored companion image.
func buildNoteRelativeCompanionAssetPath(noteURLPath, assetFileName string) string {
	return path.Join(jotesCompanionDirectoryName(path.Base(path.Clean("/"+strings.TrimPrefix(strings.TrimSpace(noteURLPath), "/")))), assetFileName)
}

// jotesCompanionRelativePathPrefix builds the directory-name prefix that note
// content should use for relative references into one note's companion asset
// folder.
//
// Parameters:
//   - noteURLPath: the absolute-style Jotes URL path of the owning note file.
//
// Returns:
//   - string: the note-relative companion-folder prefix ending in a trailing slash.
func jotesCompanionRelativePathPrefix(noteURLPath string) string {
	return buildNoteRelativeCompanionAssetPath(noteURLPath, "") + "/"
}

// isJotesCompanionDirectoryName reports whether one basename belongs to the
// reserved hidden companion-directory namespace managed by pasted-image note
// uploads.
//
// Parameters:
//   - entryName: the directory-entry basename to classify.
//
// Returns:
//   - bool: true when entryName starts with the reserved ".jotes-" prefix.
func isJotesCompanionDirectoryName(entryName string) bool {
	return strings.HasPrefix(strings.TrimSpace(entryName), jotesCompanionDirectoryPrefix)
}

// pathContainsJotesCompanionDirectory reports whether one absolute-style Jotes
// URL path points at a companion directory or any descendant inside one.
//
// Parameters:
//   - urlPath: the absolute-style Jotes URL path to inspect.
//
// Returns:
//   - bool: true when any path segment begins with the reserved ".jotes-" prefix.
func pathContainsJotesCompanionDirectory(urlPath string) bool {
	for _, segment := range strings.Split(path.Clean("/"+strings.TrimPrefix(strings.TrimSpace(urlPath), "/")), "/") {
		if isJotesCompanionDirectoryName(segment) {
			return true
		}
	}
	return false
}
