package storage

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestArchive_Basic(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	store.Write("note.md", []byte("content"))

	if err := store.Archive("note.md"); err != nil {
		t.Fatal(err)
	}

	// Original should be gone
	_, err = store.Read("note.md")
	if !errors.Is(err, ErrNotFound) {
		t.Error("original file should be gone after archive")
	}

	// Should exist in .archive/
	archivePath := filepath.Join(store.Root(), ".archive", "note.md")
	data, err := os.ReadFile(archivePath)
	if err != nil {
		t.Fatalf("archived file not found: %v", err)
	}
	if string(data) != "content" {
		t.Errorf("archived content = %q, want %q", string(data), "content")
	}
}

func TestArchive_Errors(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	t.Run("nonexistent returns ErrNotFound", func(t *testing.T) {
		err := store.Archive("nope.md")
		if !errors.Is(err, ErrNotFound) {
			t.Errorf("err = %v, want ErrNotFound", err)
		}
	})

	t.Run("directory returns ErrIsDirectory", func(t *testing.T) {
		store.CreateDir("mydir")
		err := store.Archive("mydir")
		if !errors.Is(err, ErrIsDirectory) {
			t.Errorf("err = %v, want ErrIsDirectory", err)
		}
	})

	t.Run("traversal safely normalized", func(t *testing.T) {
		// SafePath normalizes ../ so this resolves under root
		err := store.Archive("../../etc/passwd")
		// Should get ErrNotFound (file doesn't exist under root) or ErrPathTraversal
		if err == nil {
			t.Error("expected error for nonexistent traversal path")
		}
	})

	t.Run("existing archive target returns ErrAlreadyExists", func(t *testing.T) {
		if err := store.Write("clash.md", []byte("new")); err != nil {
			t.Fatalf("write source: %v", err)
		}

		archiveDir := filepath.Join(store.Root(), ".archive")
		if err := os.MkdirAll(archiveDir, 0o755); err != nil {
			t.Fatalf("mkdir archive dir: %v", err)
		}
		archivePath := filepath.Join(archiveDir, "clash.md")
		if err := os.WriteFile(archivePath, []byte("old"), 0o644); err != nil {
			t.Fatalf("write archive target: %v", err)
		}

		err := store.Archive("clash.md")
		if !errors.Is(err, ErrAlreadyExists) {
			t.Fatalf("err = %v, want ErrAlreadyExists", err)
		}

		source, err := os.ReadFile(filepath.Join(store.Root(), "clash.md"))
		if err != nil {
			t.Fatalf("source should remain after conflict: %v", err)
		}
		if string(source) != "new" {
			t.Fatalf("source content = %q, want %q", source, "new")
		}

		archived, err := os.ReadFile(archivePath)
		if err != nil {
			t.Fatalf("archive target missing: %v", err)
		}
		if string(archived) != "old" {
			t.Fatalf("archive content = %q, want %q", archived, "old")
		}
	})
}

func TestUnarchive_Basic(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	store.Write("restore.md", []byte("data"))
	store.Archive("restore.md")

	if err := store.Unarchive("restore.md"); err != nil {
		t.Fatal(err)
	}

	// Should be back
	fi, err := store.Read("restore.md")
	if err != nil {
		t.Fatal(err)
	}
	if fi.Content != "data" {
		t.Errorf("content = %q, want %q", fi.Content, "data")
	}

	// Archive copy should be gone
	archivePath := filepath.Join(store.Root(), ".archive", "restore.md")
	if _, err := os.Stat(archivePath); !os.IsNotExist(err) {
		t.Error("archive copy should be gone after unarchive")
	}
}

func TestUnarchive_Errors(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	t.Run("no archived copy returns ErrNotFound", func(t *testing.T) {
		err := store.Unarchive("nope.md")
		if !errors.Is(err, ErrNotFound) {
			t.Errorf("err = %v, want ErrNotFound", err)
		}
	})

	t.Run("existing destination returns ErrAlreadyExists", func(t *testing.T) {
		archiveDir := filepath.Join(store.Root(), ".archive")
		if err := os.MkdirAll(archiveDir, 0o755); err != nil {
			t.Fatalf("mkdir archive dir: %v", err)
		}
		archivePath := filepath.Join(archiveDir, "restore.md")
		if err := os.WriteFile(archivePath, []byte("archived"), 0o644); err != nil {
			t.Fatalf("write archive file: %v", err)
		}
		if err := os.WriteFile(filepath.Join(store.Root(), "restore.md"), []byte("current"), 0o644); err != nil {
			t.Fatalf("write current file: %v", err)
		}

		err := store.Unarchive("restore.md")
		if !errors.Is(err, ErrAlreadyExists) {
			t.Fatalf("err = %v, want ErrAlreadyExists", err)
		}

		current, err := os.ReadFile(filepath.Join(store.Root(), "restore.md"))
		if err != nil {
			t.Fatalf("destination missing after conflict: %v", err)
		}
		if string(current) != "current" {
			t.Fatalf("destination content = %q, want %q", current, "current")
		}

		archived, err := os.ReadFile(archivePath)
		if err != nil {
			t.Fatalf("archive source missing after conflict: %v", err)
		}
		if string(archived) != "archived" {
			t.Fatalf("archive content = %q, want %q", archived, "archived")
		}
	})
}

func TestListArchived_Basic(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	t.Run("lists archive contents", func(t *testing.T) {
		store.Write("f1.md", []byte("a"))
		store.Write("f2.md", []byte("b"))
		store.Archive("f1.md")
		store.Archive("f2.md")

		dir, err := store.ListArchived("")
		if err != nil {
			t.Fatal(err)
		}
		if len(dir.Entries) != 2 {
			t.Errorf("got %d entries, want 2", len(dir.Entries))
		}
	})

	t.Run("missing archive dir returns empty list", func(t *testing.T) {
		s2, _ := NewStore(t.TempDir())
		dir, err := s2.ListArchived("")
		if err != nil {
			t.Fatal(err)
		}
		if len(dir.Entries) != 0 {
			t.Errorf("got %d entries, want 0", len(dir.Entries))
		}
	})
}
