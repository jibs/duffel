package storage

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
)

func (s *Store) Archive(urlPath string) error {
	fsPath, err := s.SafePath(urlPath)
	if err != nil {
		return err
	}

	info, err := os.Stat(fsPath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return ErrNotFound
		}
		return err
	}
	if info.IsDir() {
		return ErrIsDirectory
	}

	dir := filepath.Dir(fsPath)
	archiveDir := filepath.Join(dir, ".archive")
	if err := os.MkdirAll(archiveDir, 0o755); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	dest := filepath.Join(archiveDir, filepath.Base(fsPath))
	if fileExists(dest) {
		return ErrAlreadyExists
	}

	return os.Rename(fsPath, dest)
}

func (s *Store) Unarchive(urlPath string) error {
	fsPath, err := s.SafePath(urlPath)
	if err != nil {
		return err
	}

	// The URL path should point to the original location.
	// The archived file is at .archive/<filename> in the same directory.
	dir := filepath.Dir(fsPath)
	archivePath := filepath.Join(dir, ".archive", filepath.Base(fsPath))

	if _, err := os.Stat(archivePath); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return ErrNotFound
		}
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if fileExists(fsPath) {
		return ErrAlreadyExists
	}

	return os.Rename(archivePath, fsPath)
}

// ListArchived returns entries from the .archive directory at the given path.
func (s *Store) ListArchived(urlPath string) (*DirInfo, error) {
	fsPath, err := s.SafePath(urlPath)
	if err != nil {
		return nil, err
	}

	archiveDir := filepath.Join(fsPath, ".archive")
	dirEntries, err := os.ReadDir(archiveDir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return &DirInfo{Path: urlPath, Entries: []Entry{}}, nil
		}
		return nil, err
	}

	entries := make([]Entry, 0, len(dirEntries))
	for _, de := range dirEntries {
		fi, err := de.Info()
		if err != nil {
			continue
		}
		entries = append(entries, Entry{
			Name:    de.Name(),
			IsDir:   de.IsDir(),
			Size:    fi.Size(),
			ModTime: fi.ModTime(),
		})
	}

	return &DirInfo{Path: urlPath, Entries: entries}, nil
}
