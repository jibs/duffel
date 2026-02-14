package storage

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"duffel/src/backend/internal/markdown"
)

var (
	ErrPathTraversal = errors.New("path traversal not allowed")
	ErrNotFound      = errors.New("not found")
	ErrIsDirectory   = errors.New("path is a directory")
	ErrNotDirectory  = errors.New("path is not a directory")
	ErrNotEmpty      = errors.New("directory is not empty")
	ErrIsJournal     = errors.New("journal files must be modified via journal endpoints")
	ErrAlreadyExists = errors.New("already exists")
)

type Entry struct {
	Name      string    `json:"name"`
	IsDir     bool      `json:"isDir"`
	Size      int64     `json:"size"`
	ModTime   time.Time `json:"modTime"`
	IsJournal bool      `json:"isJournal"`
}

type FileInfo struct {
	Path      string    `json:"path"`
	Content   string    `json:"content"`
	Size      int64     `json:"size"`
	ModTime   time.Time `json:"modTime"`
	IsJournal bool      `json:"isJournal"`
}

type DirInfo struct {
	Path    string  `json:"path"`
	Entries []Entry `json:"entries"`
}

type Store struct {
	root string
	mu   sync.Mutex
}

func NewStore(root string) (*Store, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolving data dir: %w", err)
	}
	if err := os.MkdirAll(absRoot, 0o755); err != nil {
		return nil, fmt.Errorf("creating data dir: %w", err)
	}
	absRoot, err = filepath.EvalSymlinks(absRoot)
	if err != nil {
		return nil, fmt.Errorf("resolving symlinks in data dir: %w", err)
	}
	return &Store{root: absRoot}, nil
}

func (s *Store) Root() string {
	return s.root
}

// SafePath resolves a URL path to a filesystem path, ensuring it stays under root.
func (s *Store) SafePath(urlPath string) (string, error) {
	cleaned := filepath.Clean("/" + urlPath)
	full := filepath.Join(s.root, cleaned)
	// Ensure the resolved path is under root
	if !isWithinRoot(s.root, full) {
		return "", ErrPathTraversal
	}
	// If path exists, resolve symlinks and re-check
	resolved, err := filepath.EvalSymlinks(full)
	if err == nil {
		if !isWithinRoot(s.root, resolved) {
			return "", ErrPathTraversal
		}
		return resolved, nil
	}
	// Path doesn't exist (writes/creates) — resolve parent to catch symlinked dirs
	parent := filepath.Dir(full)
	if resolvedParent, err := filepath.EvalSymlinks(parent); err == nil {
		if !isWithinRoot(s.root, resolvedParent) {
			return "", ErrPathTraversal
		}
	}
	return full, nil
}

func isWithinRoot(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}

	if rel == "." {
		return true
	}

	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func (s *Store) List(urlPath string, includeArchived bool) (*DirInfo, error) {
	fsPath, err := s.SafePath(urlPath)
	if err != nil {
		return nil, err
	}

	info, err := os.Stat(fsPath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if !info.IsDir() {
		return nil, ErrNotDirectory
	}

	dirEntries, err := os.ReadDir(fsPath)
	if err != nil {
		return nil, err
	}

	entries := make([]Entry, 0, len(dirEntries))
	for _, de := range dirEntries {
		name := de.Name()
		// Skip .archive dirs unless requested
		if name == ".archive" && !includeArchived {
			continue
		}
		// Skip hidden files
		if strings.HasPrefix(name, ".") {
			continue
		}
		fi, err := de.Info()
		if err != nil {
			continue
		}
		isJournal := false
		if !de.IsDir() {
			isJournal = isJournalFile(filepath.Join(fsPath, name))
		}
		entries = append(entries, Entry{
			Name:      name,
			IsDir:     de.IsDir(),
			Size:      fi.Size(),
			ModTime:   fi.ModTime(),
			IsJournal: isJournal,
		})
	}

	return &DirInfo{
		Path:    urlPath,
		Entries: entries,
	}, nil
}

func (s *Store) Read(urlPath string) (*FileInfo, error) {
	fsPath, err := s.SafePath(urlPath)
	if err != nil {
		return nil, err
	}

	info, err := os.Stat(fsPath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if info.IsDir() {
		return nil, ErrIsDirectory
	}

	content, err := os.ReadFile(fsPath)
	if err != nil {
		return nil, err
	}

	return &FileInfo{
		Path:      urlPath,
		Content:   string(content),
		Size:      info.Size(),
		ModTime:   info.ModTime(),
		IsJournal: isJournalFile(fsPath),
	}, nil
}

func (s *Store) Write(urlPath string, content []byte) error {
	fsPath, err := s.SafePath(urlPath)
	if err != nil {
		return err
	}

	// Check if this is a journal file — must use journal endpoints
	if fileExists(fsPath) && isJournalFile(fsPath) {
		return ErrIsJournal
	}

	if strings.HasPrefix(string(content), "---\ntype: journal\n---") {
		return ErrIsJournal
	}

	if err := markdown.Validate(content); err != nil {
		return err
	}

	// Ensure parent directory exists
	if err := os.MkdirAll(filepath.Dir(fsPath), 0o755); err != nil {
		return err
	}

	return os.WriteFile(fsPath, content, 0o644)
}

func (s *Store) Delete(urlPath string) error {
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
		entries, err := os.ReadDir(fsPath)
		if err != nil {
			return err
		}
		if len(entries) > 0 {
			return ErrNotEmpty
		}
		return os.Remove(fsPath)
	}

	return os.Remove(fsPath)
}

func (s *Store) CreateDir(urlPath string) error {
	fsPath, err := s.SafePath(urlPath)
	if err != nil {
		return err
	}

	if fileExists(fsPath) {
		return fmt.Errorf("path already exists")
	}

	return os.MkdirAll(fsPath, 0o755)
}

func (s *Store) Move(srcPath, dstPath string) error {
	srcFS, err := s.SafePath(srcPath)
	if err != nil {
		return err
	}
	dstFS, err := s.SafePath(dstPath)
	if err != nil {
		return err
	}

	if _, err := os.Stat(srcFS); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return ErrNotFound
		}
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if fileExists(dstFS) {
		return ErrAlreadyExists
	}

	if err := os.MkdirAll(filepath.Dir(dstFS), 0o755); err != nil {
		return err
	}

	return os.Rename(srcFS, dstFS)
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
