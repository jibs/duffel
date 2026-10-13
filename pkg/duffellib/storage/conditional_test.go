package storage

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestWriteConditionalKeepsFailedDraftAndPermissions(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Write("private.md", []byte("before")); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(store.Root(), "private.md")
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
	conflict := errors.New("conflict")
	_, err = store.WriteConditional("private.md", []byte("bad overwrite"), func(current *FileInfo) error {
		if current.Content != "before" {
			t.Errorf("current content = %s", current.Content)
		}
		return conflict
	})
	if !errors.Is(err, conflict) {
		t.Fatalf("error = %v", err)
	}
	record, err := store.WriteConditional("private.md", []byte("after"), func(current *FileInfo) error {
		if current.Content != "before" {
			t.Errorf("failed check changed the stored file")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if record.Content != "after" {
		t.Fatalf("written content = %s", record.Content)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("permissions changed to %o", info.Mode().Perm())
	}
	files, err := os.ReadDir(store.Root())
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		if len(file.Name()) > 14 && file.Name()[:14] == ".duffel-write-" {
			t.Fatalf("temporary file leaked: %s", file.Name())
		}
	}
}
