package duffellib

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"duffel/pkg/duffellib/storage"
)

func TestWorkspaceSafeReadWriteJournalAndArchive(t *testing.T) {
	ws, err := Open(Options{Root: t.TempDir()})
	if err != nil {
		t.Fatalf("Open error = %v", err)
	}

	report, err := ws.WriteVerified("knowledge/notes.md", []byte("# Notes\n\nUseful context.\n"))
	if err != nil {
		t.Fatalf("WriteVerified error = %v", err)
	}
	if !report.Valid || report.Kind != "markdown" {
		t.Fatalf("WriteVerified report = %+v", report)
	}
	file, err := ws.Read("knowledge/notes.md")
	if err != nil {
		t.Fatalf("Read error = %v", err)
	}
	if file.Content != "# Notes\n\nUseful context.\n" {
		t.Fatalf("Read content = %q", file.Content)
	}

	if err := ws.CreateJournal("knowledge/journal.md", "created notes"); err != nil {
		t.Fatalf("CreateJournal error = %v", err)
	}
	if err := ws.AppendJournal("knowledge/journal.md", "added follow-up"); err != nil {
		t.Fatalf("AppendJournal error = %v", err)
	}
	journal, err := ws.Read("knowledge/journal.md")
	if err != nil {
		t.Fatalf("Read journal error = %v", err)
	}
	if !journal.IsJournal {
		t.Fatal("journal IsJournal = false")
	}
	report, err = ws.Verify("knowledge/journal.md")
	if err != nil {
		t.Fatalf("Verify journal error = %v", err)
	}
	if !report.Valid || !report.IsJournal {
		t.Fatalf("Verify journal report = %+v", report)
	}

	if err := ws.Archive("knowledge/notes.md"); err != nil {
		t.Fatalf("Archive error = %v", err)
	}
	if _, err := ws.Read("knowledge/notes.md"); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("Read archived err = %v, want ErrNotFound", err)
	}
	if err := ws.Unarchive("knowledge/notes.md"); err != nil {
		t.Fatalf("Unarchive error = %v", err)
	}
	if _, err := ws.Read("knowledge/notes.md"); err != nil {
		t.Fatalf("Read unarchived error = %v", err)
	}
}

func TestWorkspaceRejectsPathEscape(t *testing.T) {
	root := t.TempDir()
	ws, err := Open(Options{Root: filepath.Join(root, "data")})
	if err != nil {
		t.Fatalf("Open error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "secret.md"), []byte("secret"), 0o644); err != nil {
		t.Fatalf("write secret: %v", err)
	}

	if _, err := ws.Read("../secret.md"); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("Read traversal err = %v, want ErrNotFound after canonicalization under root", err)
	}
}

func TestWorkspaceSearchDefaultsToWorkspaceCollection(t *testing.T) {
	ws, err := Open(Options{Root: t.TempDir(), Collection: "project-docs"})
	if err != nil {
		t.Fatalf("Open error = %v", err)
	}

	if ws.Collection() != "project-docs" {
		t.Fatalf("Collection = %q", ws.Collection())
	}
}
