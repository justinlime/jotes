package handlers

import (
	"log"
	"sync"
	"time"

	"jotes/models"
)

// directoryListingTTL is a backstop expiry for cached directory listings so
// the UI can recover from missed watcher events without keeping stale
// modification times or membership information for too long.
const directoryListingTTL = 2 * time.Minute

type cachedDirectoryListing struct {
	entries []models.FileEntry
	expires time.Time
}

// directoryListingCache stores per-directory rendered entry models so large
// directory pages can be served without rescanning the filesystem on every
// request.
var directoryListingCache struct {
	mu    sync.RWMutex
	items map[string]cachedDirectoryListing
}

// cloneFileEntries copies one directory-entry slice so cache callers and cache
// storage never share the same backing array.
//
// Parameters:
//   - entries: the directory-entry slice that should be duplicated.
//
// Returns:
//   - []models.FileEntry: a shallow copy of entries with an independent backing array.
func cloneFileEntries(entries []models.FileEntry) []models.FileEntry {
	if len(entries) == 0 {
		return nil
	}

	cloned := make([]models.FileEntry, len(entries))
	copy(cloned, entries)
	return cloned
}

// directoryListingCacheKey builds the stable cache key for one served
// directory listing.
//
// Parameters:
//   - rootDir: the configured filesystem root for the running server.
//   - urlPath: the cleaned request URL path for the directory being listed.
//
// Returns:
//   - string: a cache key unique to the root directory and requested URL path.
func directoryListingCacheKey(rootDir, urlPath string) string {
	return rootDir + "\x00" + urlPath
}

// cachedDirectoryEntries returns a cached directory listing when available and
// otherwise builds, stores, and returns a fresh one.
//
// Parameters:
//   - rootDir: the configured filesystem root for the running server.
//   - urlPath: the cleaned request URL path for the directory being listed.
//   - fsPath: the resolved filesystem path that corresponds to urlPath.
//
// Returns:
//   - []models.FileEntry: the directory entries ready for template rendering.
//   - error: non-nil when the directory cannot be read or converted into entry models.
func cachedDirectoryEntries(rootDir, urlPath, fsPath string) ([]models.FileEntry, error) {
	cacheKey := directoryListingCacheKey(rootDir, urlPath)
	now := time.Now()

	directoryListingCache.mu.RLock()
	cachedListing, ok := directoryListingCache.items[cacheKey]
	directoryListingCache.mu.RUnlock()
	if ok && now.Before(cachedListing.expires) {
		return cloneFileEntries(cachedListing.entries), nil
	}

	freshEntries, err := buildEntries(urlPath, fsPath)
	if err != nil {
		return nil, err
	}

	directoryListingCache.mu.Lock()
	if directoryListingCache.items == nil {
		directoryListingCache.items = make(map[string]cachedDirectoryListing)
	}
	directoryListingCache.items[cacheKey] = cachedDirectoryListing{
		entries: cloneFileEntries(freshEntries),
		expires: now.Add(directoryListingTTL),
	}
	directoryListingCache.mu.Unlock()

	return freshEntries, nil
}

// invalidateDirectoryListings clears every cached directory listing so the
// next request rebuilds entries from the current filesystem state.
//
// Parameters:
//   - none: the package-level directory-listing cache is reset in place.
//
// Returns:
//   - none: callers use this after filesystem mutations that may affect listings.
func invalidateDirectoryListings() {
	directoryListingCache.mu.Lock()
	directoryListingCache.items = nil
	directoryListingCache.mu.Unlock()
}

// WarmDirectoryListingCache starts a background build of the root directory
// listing so the first large directory page is more likely to be served from
// cache.
//
// Parameters:
//   - rootDir: the filesystem root whose "/" directory listing should be warmed.
//
// Returns:
//   - none: warming runs asynchronously in a goroutine and quietly skips failures.
func WarmDirectoryListingCache(rootDir string) {
	go func() {
		fsPath, err := resolvePath(rootDir, "/")
		if err != nil {
			log.Printf("cache: root directory warm skipped: %v", err)
			return
		}

		freshEntries, err := buildEntries("/", fsPath)
		if err != nil {
			log.Printf("cache: root directory warm failed: %v", err)
			return
		}

		directoryListingCache.mu.Lock()
		if directoryListingCache.items == nil {
			directoryListingCache.items = make(map[string]cachedDirectoryListing)
		}
		directoryListingCache.items[directoryListingCacheKey(rootDir, "/")] = cachedDirectoryListing{
			entries: cloneFileEntries(freshEntries),
			expires: time.Now().Add(directoryListingTTL),
		}
		directoryListingCache.mu.Unlock()

		log.Println("cache: root directory listing warm complete")
	}()
}
