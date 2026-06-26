package storage

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// withPathLock serializes operations on the same logical path across store
// instances and processes by flocking a hashed lock file under the data root.
func (s *Store) withPathLock(namespace, path string, fn func() error) error {
	lockDir := filepath.Join(s.root, ".duffel-locks", namespace)
	if err := os.MkdirAll(lockDir, 0o755); err != nil {
		return err
	}

	sum := sha256.Sum256([]byte(path))
	lockPath := filepath.Join(lockDir, hex.EncodeToString(sum[:])+".lock")

	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		return fmt.Errorf("acquiring %s lock: %w", namespace, err)
	}
	defer syscall.Flock(int(f.Fd()), syscall.LOCK_UN) //nolint:errcheck

	return fn()
}
