package storage

import (
	"errors"
	"strings"
	"testing"
)

func TestCreateJournal_Basic(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	t.Run("front-matter present", func(t *testing.T) {
		if err := store.CreateJournal("j1.md", "hello"); err != nil {
			t.Fatal(err)
		}
		fi, err := store.Read("j1.md")
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(fi.Content, "---\ntype: journal\n---") {
			t.Error("missing journal front-matter")
		}
	})

	t.Run("initial content gets timestamp header", func(t *testing.T) {
		store.CreateJournal("j2.md", "my entry")
		fi, _ := store.Read("j2.md")
		if !strings.Contains(fi.Content, "## ") {
			t.Error("missing timestamp header")
		}
		if !strings.Contains(fi.Content, "my entry") {
			t.Error("missing initial content")
		}
	})

	t.Run("empty content produces just front-matter", func(t *testing.T) {
		store.CreateJournal("j3.md", "")
		fi, _ := store.Read("j3.md")
		if fi.Content != journalFrontMatter {
			t.Errorf("content = %q, want just front-matter", fi.Content)
		}
	})

	t.Run("already exists returns error", func(t *testing.T) {
		store.CreateJournal("j4.md", "first")
		err := store.CreateJournal("j4.md", "second")
		if err == nil {
			t.Error("expected error for existing journal")
		}
	})
}

func TestCreateJournal_Validation(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	t.Run("null bytes rejected", func(t *testing.T) {
		err := store.CreateJournal("bad.md", "hello\x00world")
		if err == nil {
			t.Error("expected error for null bytes")
		}
	})

	t.Run("path traversal resolves safely", func(t *testing.T) {
		err := store.CreateJournal("../../etc/evil.md", "content")
		if err != nil {
			return // rejected — safe
		}
		// If it succeeded, verify the file is under root
		p, _ := store.SafePath("../../etc/evil.md")
		if !strings.HasPrefix(p, store.Root()) {
			t.Fatal("journal ESCAPED root")
		}
	})
}

func TestAppendJournal_Basic(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	t.Run("entry with timestamp", func(t *testing.T) {
		store.CreateJournal("a.md", "first")
		if err := store.AppendJournal("a.md", "second"); err != nil {
			t.Fatal(err)
		}
		fi, _ := store.Read("a.md")
		// Should have two ## timestamp headers
		count := strings.Count(fi.Content, "## ")
		if count < 2 {
			t.Errorf("expected at least 2 timestamp headers, got %d", count)
		}
		if !strings.Contains(fi.Content, "second") {
			t.Error("missing appended content")
		}
	})

	t.Run("separator between entries", func(t *testing.T) {
		store.CreateJournal("b.md", "entry1")
		store.AppendJournal("b.md", "entry2")
		fi, _ := store.Read("b.md")
		if !strings.Contains(fi.Content, "---\n\n##") {
			t.Error("missing separator between entries")
		}
	})

	t.Run("multiple appends structure correct", func(t *testing.T) {
		store.CreateJournal("c.md", "e1")
		store.AppendJournal("c.md", "e2")
		store.AppendJournal("c.md", "e3")
		fi, _ := store.Read("c.md")
		separators := strings.Count(fi.Content, "\n---\n\n")
		if separators < 2 {
			t.Errorf("expected at least 2 separators, got %d", separators)
		}
	})
}

func TestAppendJournal_Errors(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	t.Run("nonexistent returns ErrNotFound", func(t *testing.T) {
		err := store.AppendJournal("nope.md", "content")
		if !errors.Is(err, ErrNotFound) {
			t.Errorf("err = %v, want ErrNotFound", err)
		}
	})

	t.Run("non-journal file returns error", func(t *testing.T) {
		store.Write("regular.md", []byte("text"))
		err := store.AppendJournal("regular.md", "content")
		if err == nil {
			t.Error("expected error for non-journal file")
		}
	})

	t.Run("content validation enforced", func(t *testing.T) {
		store.CreateJournal("val.md", "ok")
		err := store.AppendJournal("val.md", "bad\x00content")
		if err == nil {
			t.Error("expected error for null bytes in append")
		}
	})
}

func TestIsJournalFile(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	t.Run("with front-matter returns true", func(t *testing.T) {
		store.CreateJournal("j.md", "entry")
		fsPath, _ := store.SafePath("j.md")
		if !isJournalFile(fsPath) {
			t.Error("expected true for journal file")
		}
	})

	t.Run("without front-matter returns false", func(t *testing.T) {
		store.Write("r.md", []byte("regular"))
		fsPath, _ := store.SafePath("r.md")
		if isJournalFile(fsPath) {
			t.Error("expected false for regular file")
		}
	})

	t.Run("partial front-matter returns false", func(t *testing.T) {
		store.Write("p.md", []byte("---\ntype: not-journal\n---"))
		fsPath, _ := store.SafePath("p.md")
		if isJournalFile(fsPath) {
			t.Error("expected false for partial front-matter")
		}
	})

	t.Run("nonexistent returns false", func(t *testing.T) {
		fsPath, _ := store.SafePath("ghost.md")
		if isJournalFile(fsPath) {
			t.Error("expected false for nonexistent file")
		}
	})
}
