package storage

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// =============================================================================
// Security regression tests — these verify that previously confirmed
// vulnerabilities remain fixed. They use t.Errorf to fail CI on regression.
// =============================================================================

// TestSecurity_JournalFrontMatterInjection demonstrates that a regular Write
// can create a file that isJournalFile() treats as a journal, permanently
// locking it from future Write() calls.
//
// Root cause: isJournalFile() uses string prefix matching on content, and
// Write() doesn't strip or reject journal front-matter in user content.
//
// Fix: either reject content starting with journal front-matter in Write(),
// or use a sidecar/xattr to track journal status instead of content inspection.
func TestSecurity_JournalFrontMatterInjection(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	injected := "---\ntype: journal\n---\n\nFake journal content"
	err = store.Write("injected.md", []byte(injected))
	if err != nil {
		// If Write rejects journal front-matter content, the issue is fixed
		t.Logf("Write correctly rejected journal front-matter content: %v", err)
		return
	}

	fi, err := store.Read("injected.md")
	if err != nil {
		t.Fatal(err)
	}
	if !fi.IsJournal {
		return // not detected as journal — no issue
	}

	err = store.Write("injected.md", []byte("updated content"))
	if err == nil {
		return // Write succeeded — no issue
	}

	// KNOWN ISSUE: file is permanently locked from Write()
	t.Errorf("journal front-matter injection — Write() created a file "+
		"that isJournalFile() treats as a journal, locking further Write() calls. "+
		"Error: %v", err)
}

// TestSecurity_MoveRaceCondition demonstrates that concurrent Move operations
// to the same destination can cause one to silently overwrite the other
// (TOCTOU between fileExists check and os.Rename).
//
// Root cause: Move() checks fileExists(dst) then calls os.Rename() without
// holding a lock, so concurrent moves can both pass the check.
//
// Fix: use os.Link + os.Remove (atomic on same FS), or file locking.
func TestSecurity_MoveRaceCondition(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	const iterations = 50
	overwrites := 0

	for range iterations {
		nameA := "race_a.md"
		nameB := "race_b.md"
		dest := "race_dest.md"

		_ = store.Delete(dest)
		_ = store.Delete(nameA)
		_ = store.Delete(nameB)

		store.Write(nameA, []byte("content-A"))
		store.Write(nameB, []byte("content-B"))

		var wg sync.WaitGroup
		var errA, errB error

		wg.Add(2)
		go func() {
			defer wg.Done()
			errA = store.Move(nameA, dest)
		}()
		go func() {
			defer wg.Done()
			errB = store.Move(nameB, dest)
		}()
		wg.Wait()

		if errA == nil && errB == nil {
			overwrites++
		}
	}

	if overwrites > 0 {
		t.Errorf("Move TOCTOU race — %d/%d iterations had both "+
			"moves succeed to the same dest (silent data loss)", overwrites, iterations)
	}
}

// TestSecurity_JournalAppendRaceCondition demonstrates that concurrent
// AppendJournal calls can lose entries (read-modify-write without locking).
//
// Root cause: AppendJournal reads file, appends in memory, writes back.
// Concurrent calls read the same version and overwrite each other.
//
// Fix: use file locking (flock) or append-only file operations.
func TestSecurity_JournalAppendRaceCondition(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	store.CreateJournal("race-journal.md", "initial")

	const concurrent = 20
	var wg sync.WaitGroup
	wg.Add(concurrent)

	for i := range concurrent {
		go func(n int) {
			defer wg.Done()
			content := strings.Repeat("x", 100)
			_ = store.AppendJournal("race-journal.md", content)
		}(i)
	}
	wg.Wait()

	fi, err := store.Read("race-journal.md")
	if err != nil {
		t.Fatal(err)
	}

	separators := strings.Count(fi.Content, "\n---\n\n")
	if separators < concurrent {
		t.Errorf("journal append race — expected %d separators, "+
			"got %d. Lost %d entries.", concurrent, separators, concurrent-separators)
	}
}

// TestSecurity_SymlinkReadEscape verifies that symlinks inside the store root
// cannot be used to read files outside root.
//
// Root cause: SafePath validates the lexical path but does not call
// filepath.EvalSymlinks, so OS-level symlink resolution bypasses the check.
//
// Note: symlinks can only be created by external means (not via the API),
// so exploitation requires filesystem access to the store directory.
//
// Fix: call filepath.EvalSymlinks in SafePath and re-check the prefix.
func TestSecurity_SymlinkReadEscape(t *testing.T) {
	root := t.TempDir()
	store, err := NewStore(filepath.Join(root, "data"))
	if err != nil {
		t.Fatal(err)
	}

	secret := filepath.Join(root, "secret.txt")
	os.WriteFile(secret, []byte("TOP SECRET"), 0o644)

	symlink := filepath.Join(store.Root(), "link.md")
	if err := os.Symlink(secret, symlink); err != nil {
		t.Skipf("cannot create symlink: %v", err)
	}

	fi, err := store.Read("link.md")
	if err != nil {
		return // rejected — safe
	}

	if fi.Content == "TOP SECRET" {
		t.Errorf("symlink read escape — Read('link.md') returned "+
			"content from %s (outside root). SafePath does not resolve symlinks.", secret)
	}
}

// TestSecurity_SymlinkWriteEscape verifies that writing through a symlink
// cannot modify files outside root.
//
// Same root cause as SymlinkReadEscape.
func TestSecurity_SymlinkWriteEscape(t *testing.T) {
	root := t.TempDir()
	store, err := NewStore(filepath.Join(root, "data"))
	if err != nil {
		t.Fatal(err)
	}

	target := filepath.Join(root, "target.txt")
	os.WriteFile(target, []byte("original"), 0o644)

	symlink := filepath.Join(store.Root(), "link.md")
	if err := os.Symlink(target, symlink); err != nil {
		t.Skipf("cannot create symlink: %v", err)
	}

	_ = store.Write("link.md", []byte("OVERWRITTEN"))

	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) == "OVERWRITTEN" {
		t.Errorf("symlink write escape — Write('link.md') modified "+
			"%s (outside root). SafePath does not resolve symlinks.", target)
	}
}

// =============================================================================
// Confirmed-safe tests — these verify that specific attack vectors are blocked.
// =============================================================================

// TestSecurity_SafePathPrefixBoundary checks that SafePath rejects paths that
// resolve through a symlink to a sibling directory with a shared prefix.
// e.g., root="/tmp/data" must not allow access to "/tmp/data-evil/secret".
func TestSecurity_SafePathPrefixBoundary(t *testing.T) {
	root := t.TempDir()

	dataDir := filepath.Join(root, "data")
	evilDir := filepath.Join(root, "data-evil")
	os.MkdirAll(dataDir, 0o755)
	os.MkdirAll(evilDir, 0o755)
	os.WriteFile(filepath.Join(evilDir, "secret.md"), []byte("SECRET"), 0o644)

	store, err := NewStore(dataDir)
	if err != nil {
		t.Fatal(err)
	}

	symlink := filepath.Join(store.Root(), "escape")
	if err := os.Symlink(evilDir, symlink); err != nil {
		t.Skipf("cannot create symlink: %v", err)
	}

	t.Run("existing leaf", func(t *testing.T) {
		_, err := store.SafePath("escape/secret.md")
		if !errors.Is(err, ErrPathTraversal) {
			t.Fatalf("err = %v, want ErrPathTraversal", err)
		}
	})

	t.Run("missing leaf still blocked via symlinked parent", func(t *testing.T) {
		_, err := store.SafePath("escape/new.md")
		if !errors.Is(err, ErrPathTraversal) {
			t.Fatalf("err = %v, want ErrPathTraversal", err)
		}
	})

	t.Run("writes through symlinked parent are blocked", func(t *testing.T) {
		err := store.Write("escape/write.md", []byte("blocked"))
		if !errors.Is(err, ErrPathTraversal) {
			t.Fatalf("write err = %v, want ErrPathTraversal", err)
		}

		if _, statErr := os.Stat(filepath.Join(evilDir, "write.md")); statErr == nil {
			t.Fatal("write escaped root through sibling-prefix symlink")
		}
	})
}

// TestSecurity_WriteCannotEscapeRoot proves Write with traversal paths
// never creates files outside the store root.
func TestSecurity_WriteCannotEscapeRoot(t *testing.T) {
	root := t.TempDir()
	store, err := NewStore(filepath.Join(root, "data"))
	if err != nil {
		t.Fatal(err)
	}

	sentinel := filepath.Join(root, "sentinel.md")
	os.WriteFile(sentinel, []byte("original"), 0o644)

	attacks := []string{
		"../sentinel.md",
		"../../sentinel.md",
		"sub/../../sentinel.md",
		"../data/../sentinel.md",
	}

	for _, attack := range attacks {
		t.Run(attack, func(t *testing.T) {
			_ = store.Write(attack, []byte("HACKED"))

			data, err := os.ReadFile(sentinel)
			if err != nil {
				t.Fatalf("sentinel disappeared: %v", err)
			}
			if string(data) != "original" {
				t.Fatalf("SENTINEL OVERWRITTEN via Write(%q)", attack)
			}
		})
	}
}

// TestSecurity_MoveCannotEscapeRoot proves Move with traversal paths
// never places files outside the store root.
func TestSecurity_MoveCannotEscapeRoot(t *testing.T) {
	root := t.TempDir()
	store, err := NewStore(filepath.Join(root, "data"))
	if err != nil {
		t.Fatal(err)
	}

	attacks := []string{
		"../escaped.md",
		"../../escaped.md",
		"sub/../../escaped.md",
	}

	for _, attack := range attacks {
		t.Run("dest="+attack, func(t *testing.T) {
			store.Write("src.md", []byte("content"))
			_ = store.Move("src.md", attack)

			escaped := filepath.Join(root, "escaped.md")
			if _, err := os.Stat(escaped); err == nil {
				t.Fatalf("FILE ESCAPED ROOT via Move(src, %q)", attack)
			}
		})
	}
}

// TestSecurity_CreateJournalCannotEscapeRoot proves CreateJournal with
// traversal paths never creates files outside the store root.
func TestSecurity_CreateJournalCannotEscapeRoot(t *testing.T) {
	root := t.TempDir()
	store, err := NewStore(filepath.Join(root, "data"))
	if err != nil {
		t.Fatal(err)
	}

	attacks := []string{
		"../escaped-journal.md",
		"../../escaped-journal.md",
		"sub/../../escaped-journal.md",
	}

	for _, attack := range attacks {
		t.Run(attack, func(t *testing.T) {
			_ = store.CreateJournal(attack, "entry")

			escaped := filepath.Join(root, "escaped-journal.md")
			if _, err := os.Stat(escaped); err == nil {
				t.Fatalf("JOURNAL ESCAPED ROOT via CreateJournal(%q)", attack)
			}
		})
	}
}

// TestSecurity_CreateDirCannotEscapeRoot proves CreateDir with traversal
// paths never creates directories outside the store root.
func TestSecurity_CreateDirCannotEscapeRoot(t *testing.T) {
	root := t.TempDir()
	store, err := NewStore(filepath.Join(root, "data"))
	if err != nil {
		t.Fatal(err)
	}

	attacks := []string{
		"../escaped-dir",
		"../../escaped-dir",
		"sub/../../escaped-dir",
	}

	for _, attack := range attacks {
		t.Run(attack, func(t *testing.T) {
			_ = store.CreateDir(attack)

			escaped := filepath.Join(root, "escaped-dir")
			if _, err := os.Stat(escaped); err == nil {
				t.Fatalf("DIR ESCAPED ROOT via CreateDir(%q)", attack)
			}
		})
	}
}
