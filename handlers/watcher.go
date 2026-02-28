package handlers

import (
	"errors"
	"log"
	"os"
	"path/filepath"
	"syscall"

	"github.com/fsnotify/fsnotify"
)

// StartWatcher registers recursive filesystem watches beneath rootDir and
// invalidates cached directory-listing data when filesystem changes would make
// the current in-memory listing metadata stale.
//
// Parameters:
//   - rootDir: the single filesystem directory served by Jotes.
//
// Returns:
//   - error: non-nil when the watcher cannot be created.
func StartWatcher(rootDir string) error {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}

	if err := watchRecursive(watcher, rootDir); err != nil {
		log.Printf("watcher: could not watch %s: %v", rootDir, err)
	}

	go func() {
		defer watcher.Close()
		for {
			select {
			case event, ok := <-watcher.Events:
				if !ok {
					return
				}
				handleEvent(watcher, event)
			case err, ok := <-watcher.Errors:
				if !ok {
					return
				}
				log.Printf("watcher: %v", err)
			}
		}
	}()

	return nil
}

// watchRecursive adds fsnotify watches for dir and every nested directory it
// currently contains.
//
// Parameters:
//   - watcher: the fsnotify watcher that receives new directory registrations.
//   - dir: the filesystem directory whose tree should be watched.
//
// Returns:
//   - error: non-nil when walking fails catastrophically; individual watch errors are logged and skipped.
func watchRecursive(watcher *fsnotify.Watcher, dir string) error {
	return filepath.WalkDir(dir, func(currentPath string, entry os.DirEntry, err error) error {
		if err != nil {
			log.Printf("watcher: skipping %s: %v", currentPath, err)
			return nil
		}
		if !entryIsDir(entry) {
			return nil
		}
		if err := watcher.Add(currentPath); err != nil {
			if errors.Is(err, syscall.ENOSPC) {
				log.Printf(
					"watcher: inotify watch limit reached (stopped at %s).\n"+
						"  Directories beyond this point will rely on the %s directory listing cache TTL for freshness.\n"+
						"  To enable full coverage, raise the kernel limit:\n"+
						"    echo fs.inotify.max_user_watches=524288 | sudo tee -a /etc/sysctl.conf\n"+
						"    sudo sysctl -p",
					currentPath,
					directoryListingTTL,
				)
				return filepath.SkipAll
			}
			log.Printf("watcher: could not add watch for %s: %v", currentPath, err)
		}
		return nil
	})
}

// handleEvent processes one fsnotify event and performs any required cache
// invalidation or watch registration.
//
// Parameters:
//   - watcher: the active fsnotify watcher so newly created directories can be added immediately.
//   - event: the filesystem event emitted by fsnotify.
//
// Returns:
//   - none: side effects are limited to logging, watch registration, and directory-listing cache invalidation.
func handleEvent(watcher *fsnotify.Watcher, event fsnotify.Event) {
	if event.Has(fsnotify.Create) {
		if info, err := os.Stat(event.Name); err == nil && info.IsDir() {
			if err := watchRecursive(watcher, event.Name); err != nil {
				log.Printf("watcher: could not watch new dir %s: %v", event.Name, err)
			}
		}
	}

	if event.Op != 0 {
		invalidateDirectoryListings()
	}
}
