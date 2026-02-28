package handlers

import "os"

// syncDirectory fsyncs one directory path after an atomic rename so the rename
// itself is durably recorded on disk before Jotes reports success.
//
// Parameters:
//   - dirPath: the filesystem directory whose metadata should be synced.
//
// Returns:
//   - error: non-nil when the directory cannot be opened or synced.
func syncDirectory(dirPath string) error {
	dir, err := os.Open(dirPath)
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}
