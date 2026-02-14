package storage

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSafePath_ValidPaths(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name    string
		input   string
		wantRel string // expected suffix relative to root
	}{
		{"empty string returns root", "", ""},
		{"simple file", "notes/hello.md", "notes/hello.md"},
		{"leading slash stripped", "/notes/hello.md", "notes/hello.md"},
		{"trailing slash stripped", "notes/", "notes"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := store.SafePath(tt.input)
			if err != nil {
				t.Fatalf("SafePath(%q) error = %v", tt.input, err)
			}
			want := filepath.Join(store.Root(), tt.wantRel)
			if got != want {
				t.Errorf("SafePath(%q) = %q, want %q", tt.input, got, want)
			}
		})
	}
}

func TestSafePath_TraversalAttacks(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	attacks := []string{
		"../../../etc/passwd",
		"notes/../../secret",
		"..",
		"notes/../../../etc/shadow",
		"..%2f..%2fetc%2fpasswd",
		"notes/..\\..\\secret",
	}
	for _, attack := range attacks {
		t.Run(attack, func(t *testing.T) {
			got, err := store.SafePath(attack)
			if errors.Is(err, ErrPathTraversal) {
				return // rejected outright — safe
			}
			if err != nil {
				t.Fatalf("SafePath(%q) unexpected error = %v", attack, err)
			}
			// If it resolved, it MUST be under root
			if !strings.HasPrefix(got, store.Root()) {
				t.Fatalf("SafePath(%q) = %q, ESCAPES root %q", attack, got, store.Root())
			}
		})
	}
}

// Traversal escape tests moved to security_test.go

func TestSafePath_EdgeCases(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	t.Run("dot returns root", func(t *testing.T) {
		got, err := store.SafePath(".")
		if err != nil {
			t.Fatalf("SafePath(\".\") error = %v", err)
		}
		if got != store.Root() {
			t.Errorf("SafePath(\".\") = %q, want root %q", got, store.Root())
		}
	})

	t.Run("double slashes normalized", func(t *testing.T) {
		got, err := store.SafePath("notes//hello.md")
		if err != nil {
			t.Fatal(err)
		}
		want := filepath.Join(store.Root(), "notes/hello.md")
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	t.Run("spaces in path", func(t *testing.T) {
		got, err := store.SafePath("my notes/file name.md")
		if err != nil {
			t.Fatal(err)
		}
		want := filepath.Join(store.Root(), "my notes/file name.md")
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})
}

func TestNewStore(t *testing.T) {
	t.Run("creates root dir if missing", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "nonexistent", "nested")
		store, err := NewStore(dir)
		if err != nil {
			t.Fatal(err)
		}
		info, err := os.Stat(store.Root())
		if err != nil {
			t.Fatalf("root dir not created: %v", err)
		}
		if !info.IsDir() {
			t.Error("root is not a directory")
		}
	})

	t.Run("returns absolute root", func(t *testing.T) {
		store, err := NewStore(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		if !filepath.IsAbs(store.Root()) {
			t.Errorf("root %q is not absolute", store.Root())
		}
	})
}

func TestRead_Basic(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	t.Run("existing file returns content", func(t *testing.T) {
		store.Write("test.md", []byte("hello"))
		fi, err := store.Read("test.md")
		if err != nil {
			t.Fatal(err)
		}
		if fi.Content != "hello" {
			t.Errorf("content = %q, want %q", fi.Content, "hello")
		}
		if fi.Size != 5 {
			t.Errorf("size = %d, want 5", fi.Size)
		}
		if fi.ModTime.IsZero() {
			t.Error("modTime is zero")
		}
	})

	t.Run("nonexistent returns ErrNotFound", func(t *testing.T) {
		_, err := store.Read("nope.md")
		if !errors.Is(err, ErrNotFound) {
			t.Errorf("err = %v, want ErrNotFound", err)
		}
	})

	t.Run("directory returns ErrIsDirectory", func(t *testing.T) {
		store.CreateDir("somedir")
		_, err := store.Read("somedir")
		if !errors.Is(err, ErrIsDirectory) {
			t.Errorf("err = %v, want ErrIsDirectory", err)
		}
	})

	t.Run("journal detection", func(t *testing.T) {
		store.CreateJournal("journal.md", "entry")
		fi, err := store.Read("journal.md")
		if err != nil {
			t.Fatal(err)
		}
		if !fi.IsJournal {
			t.Error("IsJournal = false, want true")
		}
	})
}

func TestWrite_ValidatesMarkdown(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	t.Run("null bytes rejected", func(t *testing.T) {
		err := store.Write("bad.md", []byte("hello\x00world"))
		if err == nil {
			t.Error("expected error for null bytes")
		}
	})

	t.Run("binary rejected", func(t *testing.T) {
		data := make([]byte, 100)
		for i := range data {
			data[i] = 0x01
		}
		err := store.Write("binary.md", data)
		if err == nil {
			t.Error("expected error for binary content")
		}
	})

	t.Run("valid content accepted", func(t *testing.T) {
		if err := store.Write("ok.md", []byte("# Valid")); err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("empty content accepted", func(t *testing.T) {
		if err := store.Write("empty.md", []byte("")); err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})
}

func TestWrite_RejectsJournalModification(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	store.CreateJournal("j.md", "initial")

	err = store.Write("j.md", []byte("overwrite"))
	if !errors.Is(err, ErrIsJournal) {
		t.Errorf("err = %v, want ErrIsJournal", err)
	}

	// Non-journal succeeds
	if err := store.Write("regular.md", []byte("fine")); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestWrite_CreatesParentDirs(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	if err := store.Write("a/b/c/file.md", []byte("deep")); err != nil {
		t.Fatal(err)
	}

	fi, err := store.Read("a/b/c/file.md")
	if err != nil {
		t.Fatal(err)
	}
	if fi.Content != "deep" {
		t.Errorf("content = %q, want %q", fi.Content, "deep")
	}
}

func TestList_Basic(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	store.Write("a.md", []byte("aaa"))
	store.Write("b.md", []byte("bbb"))

	t.Run("lists files", func(t *testing.T) {
		dir, err := store.List("", false)
		if err != nil {
			t.Fatal(err)
		}
		if len(dir.Entries) != 2 {
			t.Errorf("got %d entries, want 2", len(dir.Entries))
		}
	})

	t.Run("skips hidden files", func(t *testing.T) {
		// Create a hidden file directly on disk
		os.WriteFile(filepath.Join(store.Root(), ".hidden"), []byte("x"), 0o644)
		dir, err := store.List("", false)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range dir.Entries {
			if e.Name == ".hidden" {
				t.Error("hidden file should be skipped")
			}
		}
	})

	t.Run("skips .archive unless included", func(t *testing.T) {
		os.MkdirAll(filepath.Join(store.Root(), ".archive"), 0o755)
		dir, err := store.List("", false)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range dir.Entries {
			if e.Name == ".archive" {
				t.Error(".archive should be skipped when includeArchived=false")
			}
		}
	})

	t.Run("ErrNotFound for missing dir", func(t *testing.T) {
		_, err := store.List("nonexistent", false)
		if !errors.Is(err, ErrNotFound) {
			t.Errorf("err = %v, want ErrNotFound", err)
		}
	})

	t.Run("ErrNotDirectory for file", func(t *testing.T) {
		_, err := store.List("a.md", false)
		if !errors.Is(err, ErrNotDirectory) {
			t.Errorf("err = %v, want ErrNotDirectory", err)
		}
	})
}

func TestList_JournalDetection(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	store.CreateJournal("j.md", "entry")
	store.Write("regular.md", []byte("text"))

	dir, err := store.List("", false)
	if err != nil {
		t.Fatal(err)
	}

	for _, e := range dir.Entries {
		switch e.Name {
		case "j.md":
			if !e.IsJournal {
				t.Error("j.md should have IsJournal=true")
			}
		case "regular.md":
			if e.IsJournal {
				t.Error("regular.md should have IsJournal=false")
			}
		}
	}
}

func TestDelete_File(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	store.Write("del.md", []byte("bye"))

	if err := store.Delete("del.md"); err != nil {
		t.Fatal(err)
	}

	_, err = store.Read("del.md")
	if !errors.Is(err, ErrNotFound) {
		t.Error("file should be gone after delete")
	}
}

func TestDelete_NonexistentReturnsNotFound(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	err = store.Delete("nope.md")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

func TestDelete_Directory(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	t.Run("empty dir deleted", func(t *testing.T) {
		store.CreateDir("emptydir")
		if err := store.Delete("emptydir"); err != nil {
			t.Errorf("Delete empty dir = %v", err)
		}
	})

	t.Run("non-empty dir returns ErrNotEmpty", func(t *testing.T) {
		store.Write("fulldir/file.md", []byte("x"))
		err := store.Delete("fulldir")
		if !errors.Is(err, ErrNotEmpty) {
			t.Errorf("err = %v, want ErrNotEmpty", err)
		}
	})
}

func TestMove_Basic(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	t.Run("moves file successfully", func(t *testing.T) {
		store.Write("src.md", []byte("data"))
		if err := store.Move("src.md", "dst.md"); err != nil {
			t.Fatal(err)
		}
		// Source gone
		_, err := store.Read("src.md")
		if !errors.Is(err, ErrNotFound) {
			t.Error("source should be gone")
		}
		// Dest correct
		fi, err := store.Read("dst.md")
		if err != nil {
			t.Fatal(err)
		}
		if fi.Content != "data" {
			t.Errorf("content = %q, want %q", fi.Content, "data")
		}
	})

	t.Run("existing dest returns ErrAlreadyExists", func(t *testing.T) {
		store.Write("a.md", []byte("a"))
		store.Write("b.md", []byte("b"))
		err := store.Move("a.md", "b.md")
		if !errors.Is(err, ErrAlreadyExists) {
			t.Errorf("err = %v, want ErrAlreadyExists", err)
		}
	})

	t.Run("missing source returns ErrNotFound", func(t *testing.T) {
		err := store.Move("missing.md", "target.md")
		if !errors.Is(err, ErrNotFound) {
			t.Errorf("err = %v, want ErrNotFound", err)
		}
	})

	t.Run("creates dest parent dirs", func(t *testing.T) {
		store.Write("moveme.md", []byte("x"))
		if err := store.Move("moveme.md", "deep/nested/moved.md"); err != nil {
			t.Fatal(err)
		}
		fi, err := store.Read("deep/nested/moved.md")
		if err != nil {
			t.Fatal(err)
		}
		if fi.Content != "x" {
			t.Errorf("content = %q, want %q", fi.Content, "x")
		}
	})
}

func TestMove_PathTraversal(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	store.Write("ok.md", []byte("x"))

	t.Run("traversal in source returns error", func(t *testing.T) {
		err := store.Move("../../etc/passwd", "dest.md")
		// Should fail — either ErrPathTraversal or ErrNotFound (since the
		// normalized path doesn't exist under root)
		if err == nil {
			t.Error("expected error for traversal source path")
		}
	})

	t.Run("traversal in dest resolves under root", func(t *testing.T) {
		err := store.Move("ok.md", "../../etc/evil.md")
		if err == nil {
			// If it succeeded, the file must have landed under root
			p, _ := store.SafePath("../../etc/evil.md")
			if !strings.HasPrefix(p, store.Root()) {
				t.Fatal("moved file ESCAPED root")
			}
		}
		// error is also acceptable
	})
}

func TestCreateDir_Basic(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	t.Run("creates directory", func(t *testing.T) {
		if err := store.CreateDir("newdir"); err != nil {
			t.Fatal(err)
		}
		dir, err := store.List("newdir", false)
		if err != nil {
			t.Fatal(err)
		}
		if len(dir.Entries) != 0 {
			t.Error("new dir should be empty")
		}
	})

	t.Run("already exists returns error", func(t *testing.T) {
		store.CreateDir("existing")
		err := store.CreateDir("existing")
		if err == nil {
			t.Error("expected error for existing dir")
		}
	})

	t.Run("nested creation", func(t *testing.T) {
		if err := store.CreateDir("a/b/c"); err != nil {
			t.Fatal(err)
		}
		_, err := store.List("a/b/c", false)
		if err != nil {
			t.Errorf("nested dir not listable: %v", err)
		}
	})
}

func TestWrite_RejectsJournalFrontMatter(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	err = store.Write("sneaky.md", []byte("---\ntype: journal\n---\n\nfake journal"))
	if err == nil {
		t.Error("expected error when writing content with journal front-matter")
	}
}

func TestWrite_OverwriteExistingFile(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	store.Write("over.md", []byte("original"))
	if err := store.Write("over.md", []byte("updated")); err != nil {
		t.Fatal(err)
	}

	fi, err := store.Read("over.md")
	if err != nil {
		t.Fatal(err)
	}
	if fi.Content != "updated" {
		t.Errorf("content = %q, want %q", fi.Content, "updated")
	}
}

func TestRead_ReturnsCorrectPath(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	store.Write("sub/file.md", []byte("data"))

	fi, err := store.Read("sub/file.md")
	if err != nil {
		t.Fatal(err)
	}
	if fi.Path != "sub/file.md" {
		t.Errorf("path = %q, want %q", fi.Path, "sub/file.md")
	}
}

func TestList_ReturnsCorrectPath(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	store.Write("mydir/a.md", []byte("a"))

	dir, err := store.List("mydir", false)
	if err != nil {
		t.Fatal(err)
	}
	if dir.Path != "mydir" {
		t.Errorf("path = %q, want %q", dir.Path, "mydir")
	}
}

func TestList_EntryMetadata(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	store.Write("meta.md", []byte("hello world"))
	store.CreateDir("metadir")

	dir, err := store.List("", false)
	if err != nil {
		t.Fatal(err)
	}

	for _, e := range dir.Entries {
		switch e.Name {
		case "meta.md":
			if e.IsDir {
				t.Error("meta.md should not be a directory")
			}
			if e.Size != 11 {
				t.Errorf("meta.md size = %d, want 11", e.Size)
			}
			if e.ModTime.IsZero() {
				t.Error("meta.md modTime should not be zero")
			}
		case "metadir":
			if !e.IsDir {
				t.Error("metadir should be a directory")
			}
		}
	}
}

func TestMove_Directory(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	store.Write("movedir/a.md", []byte("a"))
	store.Write("movedir/b.md", []byte("b"))

	if err := store.Move("movedir", "renamed"); err != nil {
		t.Fatal(err)
	}

	// Old location gone
	_, err = store.List("movedir", false)
	if !errors.Is(err, ErrNotFound) {
		t.Error("old directory should be gone")
	}

	// New location has files
	dir, err := store.List("renamed", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(dir.Entries) != 2 {
		t.Errorf("got %d entries, want 2", len(dir.Entries))
	}
}

func TestSpecialCharFilenames(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	names := []string{
		"file with spaces.md",
		"file-with-dashes.md",
		"file_with_underscores.md",
		"UPPERCASE.md",
		"MiXeD CaSe.md",
	}

	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			if err := store.Write(name, []byte("content")); err != nil {
				t.Fatalf("Write(%q) error: %v", name, err)
			}
			fi, err := store.Read(name)
			if err != nil {
				t.Fatalf("Read(%q) error: %v", name, err)
			}
			if fi.Content != "content" {
				t.Errorf("content = %q, want %q", fi.Content, "content")
			}
		})
	}
}

func TestRoot_ReturnsAbsolutePath(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if store.Root() == "" {
		t.Error("Root() should not be empty")
	}
}

func TestDelete_EmptyDirWithPriorContents(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	// Create dir with file, delete file, then delete empty dir
	store.Write("cleanup/file.md", []byte("x"))
	store.Delete("cleanup/file.md")

	if err := store.Delete("cleanup"); err != nil {
		t.Fatalf("Delete empty dir after file removal: %v", err)
	}

	_, err = store.List("cleanup", false)
	if !errors.Is(err, ErrNotFound) {
		t.Error("directory should be gone")
	}
}
