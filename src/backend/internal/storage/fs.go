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
	// ErrUnsupportedBinary is returned when binary content is written to a path
	// whose extension is not an allowlisted image type.
	ErrUnsupportedBinary = errors.New("binary content is only allowed for image files")
)

type Entry struct {
	Name      string    `json:"name"`
	IsDir     bool      `json:"isDir"`
	Size      int64     `json:"size"`
	ModTime   time.Time `json:"modTime"`
	IsJournal bool      `json:"isJournal"`
	Kind      string    `json:"kind"`
}

type FileInfo struct {
	Path      string    `json:"path"`
	Content   string    `json:"content"`
	Size      int64     `json:"size"`
	ModTime   time.Time `json:"modTime"`
	IsJournal bool      `json:"isJournal"`
	Kind      string    `json:"kind"`
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
			Kind:      fileKind(name, isJournal, de.IsDir()),
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

	isJournal := isJournalFile(fsPath)
	return &FileInfo{
		Path:      urlPath,
		Content:   string(content),
		Size:      info.Size(),
		ModTime:   info.ModTime(),
		IsJournal: isJournal,
		Kind:      fileKind(urlPath, isJournal, false),
	}, nil
}

// RawPath resolves a URL path to its on-disk filesystem path, validating it
// stays under root and refers to an existing regular file. It lets callers
// stream file bytes (e.g. via http.ServeContent) without constructing
// filesystem paths themselves.
func (s *Store) RawPath(urlPath string) (string, error) {
	fsPath, err := s.SafePath(urlPath)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(fsPath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", ErrNotFound
		}
		return "", err
	}
	if info.IsDir() {
		return "", ErrIsDirectory
	}
	return fsPath, nil
}

// StatFile returns metadata for a file without reading its contents. Used for
// image files, where the bytes must never be routed through the string Content
// field (they are served raw instead); Content is always empty.
func (s *Store) StatFile(urlPath string) (*FileInfo, error) {
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
	isJournal := isJournalFile(fsPath)
	return &FileInfo{
		Path:      urlPath,
		Content:   "",
		Size:      info.Size(),
		ModTime:   info.ModTime(),
		IsJournal: isJournal,
		Kind:      fileKind(urlPath, isJournal, false),
	}, nil
}

func fileKind(path string, isJournal bool, isDir bool) string {
	if isDir {
		return "directory"
	}
	if isJournal {
		return "journal"
	}
	if _, ok := imageContentType(path); ok {
		return "image"
	}
	switch strings.ToLower(filepath.Ext(path)) {
	case ".md", ".markdown":
		return "markdown"
	case ".html", ".htm":
		return "html"
	default:
		return "text"
	}
}

// imageExtensions is the allowlist of image file extensions Duffel will store as
// binary, mapped to the Content-Type used when serving them. This is the single
// source of truth shared by fileKind, WriteRaw, and the raw GET handler.
var imageExtensions = map[string]string{
	".png":  "image/png",
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".gif":  "image/gif",
	".webp": "image/webp",
	".avif": "image/avif",
	".bmp":  "image/bmp",
	".ico":  "image/x-icon",
	".svg":  "image/svg+xml",
}

// imageContentType returns the Content-Type for an allowlisted image path and
// whether the extension is a recognized image type. Matching is case-insensitive.
func imageContentType(path string) (string, bool) {
	ct, ok := imageExtensions[strings.ToLower(filepath.Ext(path))]
	return ct, ok
}

// IsImagePath reports whether path has an allowlisted image extension.
func IsImagePath(path string) bool {
	_, ok := imageContentType(path)
	return ok
}

// ImageContentType returns the Content-Type for an allowlisted image path.
func ImageContentType(path string) (string, bool) {
	return imageContentType(path)
}

func (s *Store) Write(urlPath string, content []byte) error {
	if strings.HasPrefix(string(content), "---\ntype: journal\n---") {
		return ErrIsJournal
	}
	if err := markdown.Validate(content); err != nil {
		return err
	}
	return s.writeFile(urlPath, content)
}

// WriteRaw writes binary content (e.g. an image) without the text validation
// applied by Write. It still enforces SafePath and the journal guard, but
// requires the destination to have an allowlisted image extension so the text
// invariant for .md/.html/.txt files is preserved.
func (s *Store) WriteRaw(urlPath string, content []byte) error {
	if !IsImagePath(urlPath) {
		return ErrUnsupportedBinary
	}
	return s.writeFile(urlPath, content)
}

// writeFile is the shared write path: SafePath resolution, journal protection,
// parent-directory creation, and the actual write.
func (s *Store) writeFile(urlPath string, content []byte) error {
	fsPath, err := s.SafePath(urlPath)
	if err != nil {
		return err
	}

	// Check if this is a journal file — must use journal endpoints
	if fileExists(fsPath) && isJournalFile(fsPath) {
		return ErrIsJournal
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

func (s *Store) DeleteRecursive(urlPath string) error {
	if strings.Trim(urlPath, "/.") == "" {
		return ErrPathTraversal
	}

	fsPath, err := s.SafePath(urlPath)
	if err != nil {
		return err
	}
	if fsPath == s.root {
		return ErrPathTraversal
	}

	if _, err := os.Stat(fsPath); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return ErrNotFound
		}
		return err
	}

	return os.RemoveAll(fsPath)
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

	return s.withPathLock("move", dstFS, func() error {
		s.mu.Lock()
		defer s.mu.Unlock()

		if _, err := os.Stat(srcFS); err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return ErrNotFound
			}
			return err
		}

		if fileExists(dstFS) {
			return ErrAlreadyExists
		}

		if err := os.MkdirAll(filepath.Dir(dstFS), 0o755); err != nil {
			return err
		}

		if err := os.Rename(srcFS, dstFS); err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return ErrNotFound
			}
			return err
		}

		return nil
	})
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
