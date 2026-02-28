package handlers

import (
	"os"

	"golang.org/x/sys/unix"
)

// lockOpenFile acquires an exclusive advisory lock for one already-open lock
// file handle so Jotes can serialize saves for a specific note path.
//
// Parameters:
//   - file: the open lock-file handle that should be locked for exclusive access.
//
// Returns:
//   - error: non-nil when the operating system refuses the exclusive lock request.
func lockOpenFile(file *os.File) error {
	return unix.Flock(int(file.Fd()), unix.LOCK_EX)
}

// unlockOpenFile releases the advisory lock previously obtained for one open
// lock-file handle.
//
// Parameters:
//   - file: the open lock-file handle whose exclusive lock should be released.
//
// Returns:
//   - error: non-nil when the operating system refuses the unlock request.
func unlockOpenFile(file *os.File) error {
	return unix.Flock(int(file.Fd()), unix.LOCK_UN)
}
