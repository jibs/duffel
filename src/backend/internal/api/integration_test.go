package api_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"duffel/src/backend/internal/api"
	"duffel/src/backend/internal/search"
	"duffel/src/backend/internal/storage"
)

func setupTestServer(t *testing.T) (*httptest.Server, *storage.Store) {
	t.Helper()
	dir := t.TempDir()
	store, err := storage.NewStore(dir)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	router := api.NewRouter(store, func() *search.Searcher { return nil }, dir)
	return httptest.NewServer(router), store
}

func TestCreateAndReadFile(t *testing.T) {
	srv, _ := setupTestServer(t)
	defer srv.Close()

	// PUT file content (parent dirs created automatically)
	req, _ := http.NewRequest("PUT", srv.URL+"/api/fs/notes/hello.md", strings.NewReader(`{"content":"# Hello\n\nWorld"}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("PUT status = %d, want 200", resp.StatusCode)
	}

	// GET file
	resp, err = http.Get(srv.URL + "/api/fs/notes/hello.md")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	var file storage.FileInfo
	json.NewDecoder(resp.Body).Decode(&file)
	if file.Content != "# Hello\n\nWorld" {
		t.Errorf("content = %q, want %q", file.Content, "# Hello\n\nWorld")
	}
}

func TestListDirectory(t *testing.T) {
	srv, store := setupTestServer(t)
	defer srv.Close()

	store.Write("a.md", []byte("aaa"))
	store.Write("b.md", []byte("bbb"))

	resp, err := http.Get(srv.URL + "/api/fs/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	var dir storage.DirInfo
	json.NewDecoder(resp.Body).Decode(&dir)
	if len(dir.Entries) != 2 {
		t.Errorf("got %d entries, want 2", len(dir.Entries))
	}
}

func TestDeleteFile(t *testing.T) {
	srv, store := setupTestServer(t)
	defer srv.Close()

	store.Write("todelete.md", []byte("bye"))

	req, _ := http.NewRequest("DELETE", srv.URL+"/api/fs/todelete.md", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("DELETE status = %d, want 200", resp.StatusCode)
	}

	// Verify gone
	resp, err = http.Get(srv.URL + "/api/fs/todelete.md")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("GET after delete status = %d, want 404", resp.StatusCode)
	}
}

func TestArchiveUnarchive(t *testing.T) {
	srv, store := setupTestServer(t)
	defer srv.Close()

	store.Write("archiveme.md", []byte("content"))

	// Archive
	resp, err := http.Post(srv.URL+"/api/archive/archiveme.md", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("archive status = %d, want 200", resp.StatusCode)
	}

	// File should be gone
	resp, err = http.Get(srv.URL + "/api/fs/archiveme.md")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("GET after archive = %d, want 404", resp.StatusCode)
	}

	// Unarchive
	resp, err = http.Post(srv.URL+"/api/unarchive/archiveme.md", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("unarchive status = %d, want 200", resp.StatusCode)
	}

	// File should be back
	resp, err = http.Get(srv.URL + "/api/fs/archiveme.md")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET after unarchive = %d, want 200", resp.StatusCode)
	}
}

func TestJournalCreateAndAppend(t *testing.T) {
	srv, _ := setupTestServer(t)
	defer srv.Close()

	// Create journal
	resp, err := http.Post(srv.URL+"/api/journal/daily.md", "application/json",
		strings.NewReader(`{"content":"First entry"}`))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create journal status = %d, want 201", resp.StatusCode)
	}

	// Append to journal
	resp, err = http.Post(srv.URL+"/api/journal/daily.md/append", "application/json",
		strings.NewReader(`{"content":"Second entry"}`))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("append journal status = %d, want 200", resp.StatusCode)
	}

	// Read and verify
	resp, err = http.Get(srv.URL + "/api/fs/daily.md")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	var file storage.FileInfo
	json.NewDecoder(resp.Body).Decode(&file)

	if !strings.Contains(file.Content, "type: journal") {
		t.Error("missing journal front-matter")
	}
	if !strings.Contains(file.Content, "First entry") {
		t.Error("missing first entry")
	}
	if !strings.Contains(file.Content, "Second entry") {
		t.Error("missing second entry")
	}
	if !strings.Contains(file.Content, "---\n\n##") {
		t.Error("missing separator between entries")
	}
}

func TestJournalRejectDirectPut(t *testing.T) {
	srv, store := setupTestServer(t)
	defer srv.Close()

	store.CreateJournal("journal.md", "initial")

	req, _ := http.NewRequest("PUT", srv.URL+"/api/fs/journal.md", strings.NewReader(`{"content":"overwrite"}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("PUT on journal status = %d, want 400", resp.StatusCode)
	}
}

func TestCreateDirectory(t *testing.T) {
	srv, _ := setupTestServer(t)
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/api/fs/newfolder", "application/json",
		strings.NewReader(`{"type":"directory"}`))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create dir status = %d, want 201", resp.StatusCode)
	}

	// Should be listable
	resp, err = http.Get(srv.URL + "/api/fs/newfolder")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	var dir storage.DirInfo
	json.NewDecoder(resp.Body).Decode(&dir)
	if len(dir.Entries) != 0 {
		t.Errorf("new dir should be empty, got %d entries", len(dir.Entries))
	}
}

func TestMoveFile(t *testing.T) {
	srv, store := setupTestServer(t)
	defer srv.Close()

	store.Write("movesrc.md", []byte("moving"))

	t.Run("success", func(t *testing.T) {
		resp, err := http.Post(srv.URL+"/api/move/movesrc.md", "application/json",
			strings.NewReader(`{"destination":"movedst.md"}`))
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want 200", resp.StatusCode)
		}
	})

	t.Run("dest conflict 409", func(t *testing.T) {
		store.Write("x.md", []byte("a"))
		store.Write("y.md", []byte("b"))
		resp, err := http.Post(srv.URL+"/api/move/x.md", "application/json",
			strings.NewReader(`{"destination":"y.md"}`))
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusConflict {
			t.Fatalf("status = %d, want 409", resp.StatusCode)
		}
	})

	t.Run("missing source 404", func(t *testing.T) {
		resp, err := http.Post(srv.URL+"/api/move/ghost.md", "application/json",
			strings.NewReader(`{"destination":"out.md"}`))
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("status = %d, want 404", resp.StatusCode)
		}
	})

	t.Run("empty dest 400", func(t *testing.T) {
		store.Write("e.md", []byte("e"))
		resp, err := http.Post(srv.URL+"/api/move/e.md", "application/json",
			strings.NewReader(`{"destination":""}`))
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400", resp.StatusCode)
		}
	})
}

func TestPutPathTraversal(t *testing.T) {
	srv, _ := setupTestServer(t)
	defer srv.Close()

	// Chi router normalizes URLs, so ../../ gets cleaned before reaching the handler.
	// The SafePath function also normalizes, so traversal attempts resolve safely under root.
	// This test verifies that the combination doesn't produce an error — the path is safely handled.
	req, _ := http.NewRequest("PUT", srv.URL+"/api/fs/..%2F..%2Fetc%2Fpasswd", strings.NewReader(`{"content":"evil"}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	// Either 400 (if traversal detected) or 200 (if safely normalized under root) is acceptable
	if resp.StatusCode == http.StatusInternalServerError {
		t.Fatalf("unexpected 500 status")
	}
}

func TestPutBinaryContent(t *testing.T) {
	srv, _ := setupTestServer(t)
	defer srv.Close()

	req, _ := http.NewRequest("PUT", srv.URL+"/api/fs/binary.md", strings.NewReader(`{"content":"hello\u0000world"}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

func TestDeleteNonexistent(t *testing.T) {
	srv, _ := setupTestServer(t)
	defer srv.Close()

	req, _ := http.NewRequest("DELETE", srv.URL+"/api/fs/nope.md", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
}

func TestDeleteNonEmptyDir(t *testing.T) {
	srv, store := setupTestServer(t)
	defer srv.Close()

	store.Write("notempty/file.md", []byte("x"))

	req, _ := http.NewRequest("DELETE", srv.URL+"/api/fs/notempty", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

func TestGetNonexistent(t *testing.T) {
	srv, _ := setupTestServer(t)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/fs/missing.md")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
}

func TestArchiveErrors(t *testing.T) {
	srv, store := setupTestServer(t)
	defer srv.Close()

	t.Run("archive nonexistent 404", func(t *testing.T) {
		resp, err := http.Post(srv.URL+"/api/archive/nope.md", "", nil)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("status = %d, want 404", resp.StatusCode)
		}
	})

	t.Run("archive directory 400", func(t *testing.T) {
		store.CreateDir("adir")
		resp, err := http.Post(srv.URL+"/api/archive/adir", "", nil)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400", resp.StatusCode)
		}
	})

	t.Run("archive conflict 409", func(t *testing.T) {
		if err := store.Write("clash.md", []byte("first")); err != nil {
			t.Fatalf("seed source: %v", err)
		}
		if err := store.Archive("clash.md"); err != nil {
			t.Fatalf("seed archive: %v", err)
		}
		if err := store.Write("clash.md", []byte("second")); err != nil {
			t.Fatalf("seed replacement source: %v", err)
		}

		resp, err := http.Post(srv.URL+"/api/archive/clash.md", "", nil)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusConflict {
			t.Fatalf("status = %d, want 409", resp.StatusCode)
		}

		current, err := store.Read("clash.md")
		if err != nil {
			t.Fatalf("read current after conflict: %v", err)
		}
		if current.Content != "second" {
			t.Fatalf("current content = %q, want %q", current.Content, "second")
		}

		archived, err := os.ReadFile(filepath.Join(store.Root(), ".archive", "clash.md"))
		if err != nil {
			t.Fatalf("read archived after conflict: %v", err)
		}
		if string(archived) != "first" {
			t.Fatalf("archived content = %q, want %q", archived, "first")
		}
	})
}

func TestUnarchiveNonexistent(t *testing.T) {
	srv, store := setupTestServer(t)
	defer srv.Close()

	t.Run("not found 404", func(t *testing.T) {
		resp, err := http.Post(srv.URL+"/api/unarchive/nope.md", "", nil)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("status = %d, want 404", resp.StatusCode)
		}
	})

	t.Run("unarchive conflict 409", func(t *testing.T) {
		if err := store.Write("restore.md", []byte("archived")); err != nil {
			t.Fatalf("seed source: %v", err)
		}
		if err := store.Archive("restore.md"); err != nil {
			t.Fatalf("seed archive: %v", err)
		}
		if err := store.Write("restore.md", []byte("current")); err != nil {
			t.Fatalf("seed destination: %v", err)
		}

		resp, err := http.Post(srv.URL+"/api/unarchive/restore.md", "", nil)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusConflict {
			t.Fatalf("status = %d, want 409", resp.StatusCode)
		}

		current, err := store.Read("restore.md")
		if err != nil {
			t.Fatalf("read destination after conflict: %v", err)
		}
		if current.Content != "current" {
			t.Fatalf("destination content = %q, want %q", current.Content, "current")
		}

		archived, err := os.ReadFile(filepath.Join(store.Root(), ".archive", "restore.md"))
		if err != nil {
			t.Fatalf("read archive after conflict: %v", err)
		}
		if string(archived) != "archived" {
			t.Fatalf("archive content = %q, want %q", archived, "archived")
		}
	})
}

func TestJournalAppendErrors(t *testing.T) {
	srv, _ := setupTestServer(t)
	defer srv.Close()

	t.Run("append to nonexistent 404", func(t *testing.T) {
		resp, err := http.Post(srv.URL+"/api/journal/nope.md/append", "application/json",
			strings.NewReader(`{"content":"entry"}`))
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("status = %d, want 404", resp.StatusCode)
		}
	})

	t.Run("empty content 400", func(t *testing.T) {
		// Create a journal first
		resp, err := http.Post(srv.URL+"/api/journal/forappend.md", "application/json",
			strings.NewReader(`{"content":"init"}`))
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()

		resp, err = http.Post(srv.URL+"/api/journal/forappend.md/append", "application/json",
			strings.NewReader(`{"content":""}`))
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400", resp.StatusCode)
		}
	})
}

func TestAgentVersion(t *testing.T) {
	srv, _ := setupTestServer(t)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/agent/version")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	ct := resp.Header.Get("Content-Type")
	if !strings.Contains(ct, "text/plain") {
		t.Errorf("Content-Type = %q, want text/plain", ct)
	}
}

func TestAgentScript(t *testing.T) {
	srv, _ := setupTestServer(t)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/agent/script")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var body strings.Builder
	io.Copy(&body, resp.Body)
	if !strings.Contains(body.String(), "DUFFEL_URL=") {
		t.Error("script missing DUFFEL_URL=")
	}

	cd := resp.Header.Get("Content-Disposition")
	if cd == "" {
		t.Error("missing Content-Disposition header")
	}
}

func TestAgentSnippet(t *testing.T) {
	srv, _ := setupTestServer(t)
	defer srv.Close()

	t.Run("without path", func(t *testing.T) {
		resp, err := http.Get(srv.URL + "/api/agent/snippet")
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want 200", resp.StatusCode)
		}

		ct := resp.Header.Get("Content-Type")
		if !strings.Contains(ct, "text/markdown") {
			t.Errorf("Content-Type = %q, want text/markdown", ct)
		}
	})

	t.Run("with path param", func(t *testing.T) {
		resp, err := http.Get(srv.URL + "/api/agent/snippet?path=myproject")
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()

		var body strings.Builder
		io.Copy(&body, resp.Body)
		if !strings.Contains(body.String(), "myproject") {
			t.Error("snippet with path should contain project path")
		}
	})
}

func TestCORSHeaders(t *testing.T) {
	srv, _ := setupTestServer(t)
	defer srv.Close()

	t.Run("same-origin request gets CORS headers", func(t *testing.T) {
		req, _ := http.NewRequest("GET", srv.URL+"/api/agent/version", nil)
		req.Header.Set("Origin", srv.URL)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.Header.Get("Access-Control-Allow-Origin") != srv.URL {
			t.Fatalf("Access-Control-Allow-Origin = %q, want %q", resp.Header.Get("Access-Control-Allow-Origin"), srv.URL)
		}
	})

	t.Run("disallowed origin gets no CORS allow header", func(t *testing.T) {
		req, _ := http.NewRequest("GET", srv.URL+"/api/agent/version", nil)
		req.Header.Set("Origin", "https://evil.example")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if got := resp.Header.Get("Access-Control-Allow-Origin"); got != "" {
			t.Fatalf("Access-Control-Allow-Origin = %q, want empty", got)
		}
	})

	t.Run("OPTIONS same-origin returns 200 with methods", func(t *testing.T) {
		req, _ := http.NewRequest("OPTIONS", srv.URL+"/api/fs/test", nil)
		req.Header.Set("Origin", srv.URL)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("OPTIONS status = %d, want 200", resp.StatusCode)
		}
		methods := resp.Header.Get("Access-Control-Allow-Methods")
		if !strings.Contains(methods, "GET") || !strings.Contains(methods, "POST") {
			t.Errorf("methods = %q, want GET,POST etc", methods)
		}
		if resp.Header.Get("Access-Control-Allow-Origin") != srv.URL {
			t.Fatalf("Access-Control-Allow-Origin = %q, want %q", resp.Header.Get("Access-Control-Allow-Origin"), srv.URL)
		}
	})

	t.Run("OPTIONS disallowed origin returns 403", func(t *testing.T) {
		req, _ := http.NewRequest("OPTIONS", srv.URL+"/api/fs/test", nil)
		req.Header.Set("Origin", "https://evil.example")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusForbidden {
			t.Fatalf("OPTIONS status = %d, want 403", resp.StatusCode)
		}
	})
}

func TestCrossOriginWriteBlocked(t *testing.T) {
	srv, _ := setupTestServer(t)
	defer srv.Close()

	t.Run("same-origin write succeeds", func(t *testing.T) {
		req, _ := http.NewRequest("PUT", srv.URL+"/api/fs/allowed.md", strings.NewReader(`{"content":"ok"}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Origin", srv.URL)

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want 200", resp.StatusCode)
		}
	})

	t.Run("cross-origin write blocked with 403", func(t *testing.T) {
		req, _ := http.NewRequest("PUT", srv.URL+"/api/fs/blocked.md", strings.NewReader(`{"content":"nope"}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Origin", "https://evil.example")

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusForbidden {
			t.Fatalf("status = %d, want 403", resp.StatusCode)
		}

		check, err := http.Get(srv.URL + "/api/fs/blocked.md")
		if err != nil {
			t.Fatal(err)
		}
		check.Body.Close()
		if check.StatusCode != http.StatusNotFound {
			t.Fatalf("GET blocked.md status = %d, want 404", check.StatusCode)
		}
	})
}

func TestGetArchivedListing(t *testing.T) {
	srv, store := setupTestServer(t)
	defer srv.Close()

	store.Write("visible.md", []byte("yes"))
	store.Write("hidden.md", []byte("bye"))
	store.Archive("hidden.md")

	t.Run("without archived excludes", func(t *testing.T) {
		resp, err := http.Get(srv.URL + "/api/fs/")
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var dir storage.DirInfo
		json.NewDecoder(resp.Body).Decode(&dir)
		for _, e := range dir.Entries {
			if e.Name == "hidden.md" {
				t.Error("archived file should not appear without ?archived=true")
			}
		}
	})

	t.Run("with archived includes", func(t *testing.T) {
		resp, err := http.Get(srv.URL + "/api/fs/?archived=true")
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var dir storage.DirInfo
		json.NewDecoder(resp.Body).Decode(&dir)
		found := false
		for _, e := range dir.Entries {
			if e.Name == "hidden.md" {
				found = true
			}
		}
		if !found {
			t.Error("archived file should appear with ?archived=true")
		}
	})
}

func TestJournalIsJournalField(t *testing.T) {
	srv, store := setupTestServer(t)
	defer srv.Close()

	store.CreateJournal("jtest.md", "entry")
	store.Write("regular.md", []byte("text"))

	t.Run("journal file has isJournal true", func(t *testing.T) {
		resp, err := http.Get(srv.URL + "/api/fs/jtest.md")
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var file storage.FileInfo
		json.NewDecoder(resp.Body).Decode(&file)
		if !file.IsJournal {
			t.Error("isJournal should be true for journal file")
		}
	})

	t.Run("regular file has isJournal false", func(t *testing.T) {
		resp, err := http.Get(srv.URL + "/api/fs/regular.md")
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var file storage.FileInfo
		json.NewDecoder(resp.Body).Decode(&file)
		if file.IsJournal {
			t.Error("isJournal should be false for regular file")
		}
	})
}

func TestSearchWithoutSearcher(t *testing.T) {
	srv, _ := setupTestServer(t)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/search?q=test")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	// searcher is nil in tests, so search should be unavailable
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("search status = %d, want 503", resp.StatusCode)
	}
}

func TestSearchEmptyQuery(t *testing.T) {
	srv, _ := setupTestServer(t)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/search")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("search without q status = %d, want 400", resp.StatusCode)
	}

	resp, err = http.Get(srv.URL + "/api/search?q=")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("search with empty q status = %d, want 400", resp.StatusCode)
	}
}

func TestPutInvalidJSON(t *testing.T) {
	srv, _ := setupTestServer(t)
	defer srv.Close()

	req, _ := http.NewRequest("PUT", srv.URL+"/api/fs/test.md", strings.NewReader(`not json`))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("PUT invalid JSON status = %d, want 400", resp.StatusCode)
	}
}

func TestPutReturnsFileInfo(t *testing.T) {
	srv, _ := setupTestServer(t)
	defer srv.Close()

	req, _ := http.NewRequest("PUT", srv.URL+"/api/fs/info.md", strings.NewReader(`{"content":"# Info"}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	var file storage.FileInfo
	json.NewDecoder(resp.Body).Decode(&file)
	if file.Content != "# Info" {
		t.Errorf("response content = %q, want %q", file.Content, "# Info")
	}
	if file.Size != 6 {
		t.Errorf("response size = %d, want 6", file.Size)
	}
	if file.Path == "" {
		t.Error("response path should not be empty")
	}
}

func TestPostInvalidType(t *testing.T) {
	srv, _ := setupTestServer(t)
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/api/fs/badtype", "application/json",
		strings.NewReader(`{"type":"file"}`))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("POST type=file status = %d, want 400", resp.StatusCode)
	}
}

func TestPostInvalidJSON(t *testing.T) {
	srv, _ := setupTestServer(t)
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/api/fs/badjson", "application/json",
		strings.NewReader(`not json`))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("POST invalid JSON status = %d, want 400", resp.StatusCode)
	}
}

func TestMoveInvalidJSON(t *testing.T) {
	srv, store := setupTestServer(t)
	defer srv.Close()

	store.Write("m.md", []byte("data"))

	resp, err := http.Post(srv.URL+"/api/move/m.md", "application/json",
		strings.NewReader(`not json`))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("move invalid JSON status = %d, want 400", resp.StatusCode)
	}
}

func TestMoveResponseBody(t *testing.T) {
	srv, store := setupTestServer(t)
	defer srv.Close()

	store.Write("from.md", []byte("content"))

	resp, err := http.Post(srv.URL+"/api/move/from.md", "application/json",
		strings.NewReader(`{"destination":"to.md"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	var body map[string]string
	json.NewDecoder(resp.Body).Decode(&body)
	if body["status"] != "moved" {
		t.Errorf("status = %q, want %q", body["status"], "moved")
	}
	if body["from"] != "from.md" {
		t.Errorf("from = %q, want %q", body["from"], "from.md")
	}
	if body["to"] != "to.md" {
		t.Errorf("to = %q, want %q", body["to"], "to.md")
	}
}

func TestDeleteResponseBody(t *testing.T) {
	srv, store := setupTestServer(t)
	defer srv.Close()

	store.Write("delme.md", []byte("bye"))

	req, _ := http.NewRequest("DELETE", srv.URL+"/api/fs/delme.md", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	var body map[string]string
	json.NewDecoder(resp.Body).Decode(&body)
	if body["status"] != "deleted" {
		t.Errorf("status = %q, want %q", body["status"], "deleted")
	}
}

func TestArchiveResponseBody(t *testing.T) {
	srv, store := setupTestServer(t)
	defer srv.Close()

	store.Write("arch.md", []byte("data"))

	resp, err := http.Post(srv.URL+"/api/archive/arch.md", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	var body map[string]string
	json.NewDecoder(resp.Body).Decode(&body)
	if body["status"] != "archived" {
		t.Errorf("status = %q, want %q", body["status"], "archived")
	}
}

func TestUnarchiveResponseBody(t *testing.T) {
	srv, store := setupTestServer(t)
	defer srv.Close()

	store.Write("uarch.md", []byte("data"))
	store.Archive("uarch.md")

	resp, err := http.Post(srv.URL+"/api/unarchive/uarch.md", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	var body map[string]string
	json.NewDecoder(resp.Body).Decode(&body)
	if body["status"] != "unarchived" {
		t.Errorf("status = %q, want %q", body["status"], "unarchived")
	}
}

func TestJournalCreateInvalidJSON(t *testing.T) {
	srv, _ := setupTestServer(t)
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/api/journal/bad.md", "application/json",
		strings.NewReader(`not json`))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("journal create invalid JSON status = %d, want 400", resp.StatusCode)
	}
}

func TestJournalAppendInvalidJSON(t *testing.T) {
	srv, _ := setupTestServer(t)
	defer srv.Close()

	// Create journal first
	resp, err := http.Post(srv.URL+"/api/journal/forbadjson.md", "application/json",
		strings.NewReader(`{"content":"init"}`))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	resp, err = http.Post(srv.URL+"/api/journal/forbadjson.md/append", "application/json",
		strings.NewReader(`not json`))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("journal append invalid JSON status = %d, want 400", resp.StatusCode)
	}
}

func TestJournalCreateReturnsFileInfo(t *testing.T) {
	srv, _ := setupTestServer(t)
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/api/journal/created.md", "application/json",
		strings.NewReader(`{"content":"first"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d, want 201", resp.StatusCode)
	}

	var file storage.FileInfo
	json.NewDecoder(resp.Body).Decode(&file)
	if !file.IsJournal {
		t.Error("response should have isJournal=true")
	}
	if !strings.Contains(file.Content, "type: journal") {
		t.Error("response content should contain journal front-matter")
	}
	if !strings.Contains(file.Content, "first") {
		t.Error("response content should contain initial entry")
	}
}

func TestJournalAppendReturnsFileInfo(t *testing.T) {
	srv, _ := setupTestServer(t)
	defer srv.Close()

	// Create
	resp, err := http.Post(srv.URL+"/api/journal/appendret.md", "application/json",
		strings.NewReader(`{"content":"first"}`))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	// Append
	resp, err = http.Post(srv.URL+"/api/journal/appendret.md/append", "application/json",
		strings.NewReader(`{"content":"second"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	var file storage.FileInfo
	json.NewDecoder(resp.Body).Decode(&file)
	if !file.IsJournal {
		t.Error("response should have isJournal=true")
	}
	if !strings.Contains(file.Content, "second") {
		t.Error("response content should contain appended entry")
	}
}

func TestJournalCreateAlreadyExists(t *testing.T) {
	srv, _ := setupTestServer(t)
	defer srv.Close()

	// Create first
	resp, err := http.Post(srv.URL+"/api/journal/dup.md", "application/json",
		strings.NewReader(`{"content":"first"}`))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	// Create again - should fail
	resp, err = http.Post(srv.URL+"/api/journal/dup.md", "application/json",
		strings.NewReader(`{"content":"second"}`))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("duplicate journal create status = %d, want 500", resp.StatusCode)
	}
}

func TestAppendToNonJournalFile(t *testing.T) {
	srv, store := setupTestServer(t)
	defer srv.Close()

	store.Write("regular.md", []byte("plain text"))

	resp, err := http.Post(srv.URL+"/api/journal/regular.md/append", "application/json",
		strings.NewReader(`{"content":"entry"}`))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("append to non-journal status = %d, want 500", resp.StatusCode)
	}
}

func TestPutJournalFrontMatterContent(t *testing.T) {
	srv, _ := setupTestServer(t)
	defer srv.Close()

	req, _ := http.NewRequest("PUT", srv.URL+"/api/fs/sneaky.md",
		strings.NewReader(`{"content":"---\ntype: journal\n---\n\nfake journal"}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("PUT with journal front-matter status = %d, want 400", resp.StatusCode)
	}
}

func TestCreateDirectoryAlreadyExists(t *testing.T) {
	srv, store := setupTestServer(t)
	defer srv.Close()

	store.CreateDir("existing")

	resp, err := http.Post(srv.URL+"/api/fs/existing", "application/json",
		strings.NewReader(`{"type":"directory"}`))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("create existing dir status = %d, want 500", resp.StatusCode)
	}
}

func TestListSubdirectory(t *testing.T) {
	srv, store := setupTestServer(t)
	defer srv.Close()

	store.Write("sub/a.md", []byte("a"))
	store.Write("sub/b.md", []byte("b"))
	store.Write("other.md", []byte("x"))

	resp, err := http.Get(srv.URL + "/api/fs/sub")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	var dir storage.DirInfo
	json.NewDecoder(resp.Body).Decode(&dir)
	if len(dir.Entries) != 2 {
		t.Errorf("got %d entries, want 2", len(dir.Entries))
	}
}

func TestResponseContentType(t *testing.T) {
	srv, _ := setupTestServer(t)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/fs/")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	ct := resp.Header.Get("Content-Type")
	if !strings.Contains(ct, "application/json") {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
}

func TestErrorResponseFormat(t *testing.T) {
	srv, _ := setupTestServer(t)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/fs/nonexistent.md")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	var errResp struct {
		Error string `json:"error"`
		Path  string `json:"path"`
	}
	json.NewDecoder(resp.Body).Decode(&errResp)
	if errResp.Error == "" {
		t.Error("error response should have error field")
	}
	if errResp.Path == "" {
		t.Error("error response should have path field")
	}
}
