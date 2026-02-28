// Package handlers contains HTTP handlers and supporting helpers used by Jotes.
package handlers

import (
	"net/http"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"jotes/models"
)

// entryIsDir reports whether a directory entry should be treated as a real
// directory without following symlinks.
//
// Parameters:
//   - d: the directory entry returned by os.ReadDir or filepath.WalkDir.
//
// Returns:
//   - bool: true when d is an actual directory entry, otherwise false.
func entryIsDir(d os.DirEntry) bool {
	return d.IsDir()
}

// isHiddenEntryName reports whether one directory-entry name should be treated
// as hidden in the directory listing UI.
//
// Parameters:
//   - name: the base file or directory name to classify.
//
// Returns:
//   - bool: true when name begins with a dot, otherwise false.
func isHiddenEntryName(name string) bool {
	return strings.HasPrefix(name, ".")
}

type directoryEntrySnapshot struct {
	fileEntry models.FileEntry
	fullPath  string
}

const (
	directoryFilterAuxiliaryBit uint8 = 1 << iota
	directoryFilterHiddenBit
	directoryFilterJotesBit
)

type directoryTraversalCache struct {
	snapshotsByPath  map[string][]directoryEntrySnapshot
	visibilityByPath map[string]map[uint8]bool
}

// directoryVisibilityFilterMask encodes one combination of directory-filter
// toggle states into the bit index later stored inside FileEntry.VisibilityMask.
//
// Parameters:
//   - showAuxiliaryFiles: true when auxiliary files should stay visible.
//   - showHiddenFiles: true when generic dot-prefixed hidden entries should stay visible.
//   - showJotesCompanions: true when managed .jotes companion folders and their descendants should stay visible.
//
// Returns:
//   - uint8: the filter-combination bit index used by the server and browser to evaluate row visibility.
func directoryVisibilityFilterMask(showAuxiliaryFiles, showHiddenFiles, showJotesCompanions bool) uint8 {
	var filterMask uint8

	if showAuxiliaryFiles {
		filterMask |= directoryFilterAuxiliaryBit
	}
	if showHiddenFiles {
		filterMask |= directoryFilterHiddenBit
	}
	if showJotesCompanions {
		filterMask |= directoryFilterJotesBit
	}

	return filterMask
}

// directoryFilterShowsAuxiliaryFiles reports whether one encoded filter mask
// leaves auxiliary files visible in the directory table.
//
// Parameters:
//   - filterMask: the encoded directory-filter state to inspect.
//
// Returns:
//   - bool: true when auxiliary files should remain visible, otherwise false.
func directoryFilterShowsAuxiliaryFiles(filterMask uint8) bool {
	return filterMask&directoryFilterAuxiliaryBit != 0
}

// directoryFilterShowsHiddenFiles reports whether one encoded filter mask
// leaves generic hidden files and directories visible in the directory table.
//
// Parameters:
//   - filterMask: the encoded directory-filter state to inspect.
//
// Returns:
//   - bool: true when generic hidden entries should remain visible, otherwise false.
func directoryFilterShowsHiddenFiles(filterMask uint8) bool {
	return filterMask&directoryFilterHiddenBit != 0
}

// directoryFilterShowsJotesCompanions reports whether one encoded filter mask
// leaves managed .jotes companion directories and their descendants visible in
// the directory table.
//
// Parameters:
//   - filterMask: the encoded directory-filter state to inspect.
//
// Returns:
//   - bool: true when managed .jotes entries should remain visible, otherwise false.
func directoryFilterShowsJotesCompanions(filterMask uint8) bool {
	return filterMask&directoryFilterJotesBit != 0
}

// collectDirectoryEntrySnapshots enumerates one directory using the exact same
// inclusion rules that power buildEntries so cached child metadata matches the
// rows later rendered into the table.
//
// Parameters:
//   - urlPath: the current directory URL, used to build child links that match the rendered table.
//   - fsPath: the filesystem directory whose direct children should be collected.
//   - cache: per-request traversal cache used to reuse previously collected child snapshots for the same directory path.
//
// Returns:
//   - []directoryEntrySnapshot: one snapshot per child entry that survives symlink and stat checks.
//   - error: non-nil when fsPath itself cannot be read.
func collectDirectoryEntrySnapshots(urlPath, fsPath string, cache *directoryTraversalCache) ([]directoryEntrySnapshot, error) {
	if cache != nil {
		if cachedSnapshots, ok := cache.snapshotsByPath[fsPath]; ok {
			return cachedSnapshots, nil
		}
	}

	rawEntries, err := os.ReadDir(fsPath)
	if err != nil {
		return nil, err
	}

	snapshots := make([]directoryEntrySnapshot, 0, len(rawEntries))
	for _, entry := range rawEntries {
		if entry.Type()&os.ModeSymlink != 0 {
			continue
		}

		entryURLPath := path.Join(urlPath, entry.Name())
		fullPath := filepath.Join(fsPath, entry.Name())
		isDir := entryIsDir(entry)
		isJotesCompanion := pathContainsJotesCompanionDirectory(entryURLPath)

		fi, err := entry.Info()
		if err != nil {
			continue
		}

		fileEntry := models.FileEntry{
			Name:             entry.Name(),
			Path:             entryURLPath,
			IsDir:            isDir,
			ModTime:          fi.ModTime(),
			IsHidden:         isHiddenEntryName(entry.Name()) && !isJotesCompanion,
			IsJotesCompanion: isJotesCompanion,
		}

		if !isDir {
			mimeType := mimeForFile(fullPath)
			fileEntry.MIMEType = mimeType
			fileEntry.IsImage = isImage(mimeType)
			fileEntry.IsText = isText(mimeType)
			fileEntry.IsPreview = fileEntry.IsImage || fileEntry.IsText || isPDF(mimeType)
			fileEntry.IsNote = !isJotesCompanion && isRenderable(mimeType)
		}

		snapshots = append(snapshots, directoryEntrySnapshot{fileEntry: fileEntry, fullPath: fullPath})
	}

	if cache != nil {
		cache.snapshotsByPath[fsPath] = snapshots
	}
	return snapshots, nil
}

// directorySubtreeVisibleForFilters reports whether one directory should stay
// visible for the supplied filter combination by checking its current category
// flags plus whether any descendant entry would remain visible.
//
// Parameters:
//   - urlPath: the current directory URL whose direct children should be evaluated.
//   - fsPath: the absolute filesystem path of the directory whose subtree should be inspected.
//   - filterMask: the encoded directory-filter combination that should be applied.
//   - cache: per-request traversal cache used to memoize subtree visibility results for repeated checks.
//
// Returns:
//   - bool: true when the directory should remain visible for filterMask, otherwise false; unreadable directories return true conservatively so the UI does not hide uncertain content.
func directorySubtreeVisibleForFilters(urlPath, fsPath string, filterMask uint8, cache *directoryTraversalCache) bool {
	if cache != nil {
		if cachedByMask, ok := cache.visibilityByPath[fsPath]; ok {
			if cachedVisibility, ok := cachedByMask[filterMask]; ok {
				return cachedVisibility
			}
		}
	}

	snapshots, err := collectDirectoryEntrySnapshots(urlPath, fsPath, cache)
	if err != nil {
		cacheDirectoryFilterVisibility(cache, fsPath, filterMask, true)
		return true
	}
	if len(snapshots) == 0 {
		emptyDirectoryVisible := directoryFilterShowsAuxiliaryFiles(filterMask) && directoryFilterShowsHiddenFiles(filterMask)
		cacheDirectoryFilterVisibility(cache, fsPath, filterMask, emptyDirectoryVisible)
		return emptyDirectoryVisible
	}

	for _, snapshot := range snapshots {
		if entryVisibleForDirectoryFilters(snapshot.fileEntry, snapshot.fullPath, filterMask, cache) {
			cacheDirectoryFilterVisibility(cache, fsPath, filterMask, true)
			return true
		}
	}

	cacheDirectoryFilterVisibility(cache, fsPath, filterMask, false)
	return false
}

// cacheDirectoryFilterVisibility stores one computed directory-subtree
// visibility result inside the optional per-request traversal cache.
//
// Parameters:
//   - cache: the per-request traversal cache that may receive the memoized result.
//   - fsPath: the absolute filesystem path whose subtree visibility was computed.
//   - filterMask: the encoded directory-filter combination that produced the result.
//   - isVisible: the computed visibility outcome for fsPath under filterMask.
//
// Returns:
//   - none: cache is updated in place when it is available.
func cacheDirectoryFilterVisibility(cache *directoryTraversalCache, fsPath string, filterMask uint8, isVisible bool) {
	if cache == nil {
		return
	}
	if _, ok := cache.visibilityByPath[fsPath]; !ok {
		cache.visibilityByPath[fsPath] = make(map[uint8]bool, 8)
	}
	cache.visibilityByPath[fsPath][filterMask] = isVisible
}

// entryVisibleForDirectoryFilters reports whether one file or directory row
// should stay visible for a specific directory-filter combination.
//
// Parameters:
//   - fileEntry: the directory entry metadata being evaluated.
//   - fullPath: the absolute filesystem path of fileEntry, used only for recursive directory checks.
//   - filterMask: the encoded directory-filter combination that should be applied.
//   - cache: per-request traversal cache used to reuse subtree visibility results.
//
// Returns:
//   - bool: true when fileEntry should remain visible for filterMask, otherwise false.
func entryVisibleForDirectoryFilters(fileEntry models.FileEntry, fullPath string, filterMask uint8, cache *directoryTraversalCache) bool {
	if fileEntry.IsJotesCompanion {
		return directoryFilterShowsJotesCompanions(filterMask)
	}
	if fileEntry.IsHidden && !directoryFilterShowsHiddenFiles(filterMask) {
		return false
	}
	if fileEntry.IsDir {
		return directorySubtreeVisibleForFilters(fileEntry.Path, fullPath, filterMask, cache)
	}
	if !fileEntry.IsNote && !directoryFilterShowsAuxiliaryFiles(filterMask) {
		return false
	}
	return true
}

// visibilityMaskForDirectoryEntry evaluates one file or directory row against
// every supported directory-filter combination and returns the packed browser
// visibility mask for that row.
//
// Parameters:
//   - fileEntry: the directory entry metadata being evaluated.
//   - fullPath: the absolute filesystem path of fileEntry, used only for recursive directory checks.
//   - cache: per-request traversal cache used to reuse subtree visibility results.
//
// Returns:
//   - uint8: the packed visibility mask whose set bits identify filter combinations that should keep fileEntry visible.
func visibilityMaskForDirectoryEntry(fileEntry models.FileEntry, fullPath string, cache *directoryTraversalCache) uint8 {
	var visibilityMask uint8

	for filterMask := uint8(0); filterMask < 8; filterMask++ {
		if entryVisibleForDirectoryFilters(fileEntry, fullPath, filterMask, cache) {
			visibilityMask |= 1 << filterMask
		}
	}

	return visibilityMask
}

// DirHandler renders a directory listing for the requested URL path.
//
// Parameters:
//   - rootDir: the single filesystem directory exposed by the server.
//   - siteName: the branding label shown in page titles and breadcrumbs.
//   - defaultTheme: the root HTML theme class applied by the template.
//   - tmpl: any renderer capable of executing the directory template.
//
// Returns:
//   - http.HandlerFunc: a handler that serves directory listings and returns 404 for non-directories.
func DirHandler(rootDir, siteName, defaultTheme string, tmpl interface {
	ExecuteDir(http.ResponseWriter, *models.DirListing) error
}) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		urlPath := path.Clean("/" + r.URL.Path)

		fsPath, err := resolvePath(rootDir, urlPath)
		if err != nil {
			http.Error(w, "Not found", http.StatusNotFound)
			return
		}

		info, err := os.Stat(fsPath)
		if err != nil || !info.IsDir() {
			http.Error(w, "Not found", http.StatusNotFound)
			return
		}

		renderDirectoryListing(w, r, rootDir, siteName, defaultTheme, tmpl, urlPath, fsPath)
	}
}

// renderDirectoryListing renders one already-resolved directory request so
// shared routing paths can reuse the same listing logic without re-resolving
// or restatting the target directory.
//
// Parameters:
//   - w: the HTTP response writer that should receive the directory page or error.
//   - r: the current HTTP request whose user context and URL determine the listing state.
//   - rootDir: the single filesystem directory exposed by the server.
//   - siteName: the branding label shown in page titles and breadcrumbs.
//   - defaultTheme: the root HTML theme class applied by the template.
//   - tmpl: any renderer capable of executing the directory template.
//   - urlPath: the cleaned request URL path for the directory.
//   - fsPath: the resolved filesystem path for the directory.
//
// Returns:
//   - none: the function writes the rendered page or an HTTP error directly to w.
func renderDirectoryListing(w http.ResponseWriter, r *http.Request, rootDir, siteName, defaultTheme string, tmpl interface {
	ExecuteDir(http.ResponseWriter, *models.DirListing) error
}, urlPath, fsPath string) {
	entries, err := cachedDirectoryEntries(rootDir, urlPath, fsPath)
	if err != nil {
		http.Error(w, "Error reading directory", http.StatusInternalServerError)
		return
	}

	isRoot := urlPath == "/"
	title := filepath.Base(fsPath)
	if isRoot {
		title = siteName
	}

	listing := &models.DirListing{
		Title:        title,
		SiteName:     siteName,
		CurrentUser:  currentUserViewFromRequest(r),
		ShowSearch:   true,
		CurrentPath:  urlPath,
		Breadcrumbs:  buildBreadcrumbs(siteName, urlPath),
		Entries:      entries,
		IsRoot:       isRoot,
		DefaultTheme: defaultTheme,
	}

	if err := tmpl.ExecuteDir(w, listing); err != nil {
		http.Error(w, "Template error", http.StatusInternalServerError)
	}
}

// UniversalHandler intelligently routes requests to either directory listings
// or file previews based on whether the resolved filesystem path is a
// directory or a file.
//
// Parameters:
//   - rootDir: the single filesystem directory exposed by the server.
//   - siteName: the branding label shown in page titles and breadcrumbs.
//   - defaultTheme: the HTML theme class applied to the page root.
//   - tmpl: any renderer capable of executing both directory and preview templates.
//
// Returns:
//   - http.HandlerFunc: a handler that inspects the filesystem path and renders either
//     a directory listing or a file preview without reconstructing nested handlers on every request.
func UniversalHandler(rootDir, siteName, defaultTheme string, tmpl interface {
	ExecuteDir(http.ResponseWriter, *models.DirListing) error
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

		if info.IsDir() {
			renderDirectoryListing(w, r, rootDir, siteName, defaultTheme, tmpl, urlPath, fsPath)
			return
		}

		renderPreviewPage(w, r, siteName, defaultTheme, tmpl, urlPath, fsPath, info)
	}
}

// buildEntries reads one filesystem directory and converts its children into
// the sorted view-model entries expected by the directory template.
//
// Parameters:
//   - urlPath: the current directory URL, used to build child links.
//   - fsPath: the filesystem directory to enumerate.
//
// Returns:
//   - []models.FileEntry: all visible child entries sorted with directories first.
//   - error: non-nil when the directory cannot be read.
func buildEntries(urlPath, fsPath string) ([]models.FileEntry, error) {
	cache := &directoryTraversalCache{
		snapshotsByPath:  make(map[string][]directoryEntrySnapshot),
		visibilityByPath: make(map[string]map[uint8]bool),
	}
	snapshots, err := collectDirectoryEntrySnapshots(urlPath, fsPath, cache)
	if err != nil {
		return nil, err
	}

	entries := make([]models.FileEntry, 0, len(snapshots))
	for _, snapshot := range snapshots {
		fileEntry := snapshot.fileEntry
		fileEntry.VisibilityMask = visibilityMaskForDirectoryEntry(fileEntry, snapshot.fullPath, cache)
		entries = append(entries, fileEntry)
	}

	sort.Slice(entries, func(i, j int) bool {
		if entries[i].IsDir != entries[j].IsDir {
			return entries[i].IsDir
		}
		return strings.ToLower(entries[i].Name) < strings.ToLower(entries[j].Name)
	})

	return entries, nil
}

// buildBreadcrumbs creates breadcrumb navigation segments for one directory URL path.
//
// Parameters:
//   - siteName: the label used for the root breadcrumb.
//   - urlPath: the current directory or parent-directory URL path.
//
// Returns:
//   - []models.Breadcrumb: an ordered breadcrumb trail from "/" to urlPath.
func buildBreadcrumbs(siteName, urlPath string) []models.Breadcrumb {
	crumbs := []models.Breadcrumb{{Name: siteName, Path: "/"}}
	if urlPath == "/" {
		return crumbs
	}

	parts := strings.Split(strings.Trim(urlPath, "/"), "/")
	current := ""
	for _, part := range parts {
		if part == "" {
			continue
		}
		current += "/" + part
		crumbs = append(crumbs, models.Breadcrumb{Name: part, Path: current})
	}
	return crumbs
}

// buildFileBreadcrumbs creates breadcrumb navigation segments for one file URL
// path, including the file itself as the final current crumb.
//
// Parameters:
//   - siteName: the label used for the root breadcrumb.
//   - fileURLPath: the URL path of the current file whose breadcrumb trail should be built.
//
// Returns:
//   - []models.Breadcrumb: an ordered breadcrumb trail from "/" through the parent directories and ending with the file path itself.
func buildFileBreadcrumbs(siteName, fileURLPath string) []models.Breadcrumb {
	crumbs := buildBreadcrumbs(siteName, path.Dir(fileURLPath))
	crumbs = append(crumbs, models.Breadcrumb{Name: path.Base(fileURLPath), Path: fileURLPath})
	return crumbs
}
