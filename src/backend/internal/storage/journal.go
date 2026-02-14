package storage

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"duffel/src/backend/internal/markdown"
)

const journalFrontMatter = "---\ntype: journal\n---\n\n"

// isJournalFile checks if a file has journal front-matter.
func isJournalFile(fsPath string) bool {
	content, err := os.ReadFile(fsPath)
	if err != nil {
		return false
	}
	return strings.HasPrefix(string(content), "---\ntype: journal\n---")
}

func (s *Store) CreateJournal(urlPath string, initialContent string) error {
	fsPath, err := s.SafePath(urlPath)
	if err != nil {
		return err
	}

	if fileExists(fsPath) {
		return fmt.Errorf("file already exists")
	}

	if err := markdown.Validate([]byte(initialContent)); err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(fsPath), 0o755); err != nil {
		return err
	}

	content := journalFrontMatter
	if initialContent != "" {
		content += formatJournalEntry(initialContent)
	}

	return os.WriteFile(fsPath, []byte(content), 0o644)
}

func (s *Store) AppendJournal(urlPath string, entryContent string) error {
	fsPath, err := s.SafePath(urlPath)
	if err != nil {
		return err
	}

	if err := markdown.Validate([]byte(entryContent)); err != nil {
		return err
	}

	f, err := os.OpenFile(fsPath, os.O_RDWR, 0o644)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return ErrNotFound
		}
		return err
	}
	defer func() { _ = f.Close() }()

	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		return fmt.Errorf("acquiring file lock: %w", err)
	}
	defer syscall.Flock(int(f.Fd()), syscall.LOCK_UN) //nolint:errcheck

	existing, err := io.ReadAll(f)
	if err != nil {
		return err
	}

	if !strings.HasPrefix(string(existing), "---\ntype: journal\n---") {
		return fmt.Errorf("not a journal file")
	}

	newContent := string(existing)
	// Add separator if there's existing content beyond front-matter
	trimmed := strings.TrimSpace(strings.TrimPrefix(string(existing), strings.TrimSpace(journalFrontMatter)))
	if trimmed != "" {
		newContent += "\n---\n\n"
	}
	newContent += formatJournalEntry(entryContent)

	if err := f.Truncate(0); err != nil {
		return err
	}
	if _, err := f.Seek(0, 0); err != nil {
		return err
	}
	_, err = f.WriteString(newContent)
	return err
}

func formatJournalEntry(content string) string {
	ts := time.Now().Format("2006-01-02 15:04")
	return fmt.Sprintf("## %s\n\n%s\n", ts, content)
}
