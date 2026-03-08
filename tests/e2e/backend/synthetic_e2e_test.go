package backend_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

type syntheticServer struct {
	baseURL string
	cmd     *exec.Cmd
	cancel  context.CancelFunc
	logs    bytes.Buffer
}

type apiError struct {
	Error string `json:"error"`
	Path  string `json:"path"`
}

type filePayload struct {
	Path      string `json:"path"`
	Content   string `json:"content"`
	IsJournal bool   `json:"isJournal"`
}

type dirPayload struct {
	Path    string         `json:"path"`
	Entries []entryPayload `json:"entries"`
}

type entryPayload struct {
	Name  string `json:"name"`
	IsDir bool   `json:"isDir"`
}

func TestSyntheticBackendE2E(t *testing.T) {
	srv := startSyntheticServer(t)

	t.Run("filesystem lifecycle", func(t *testing.T) {
		resp := mustJSONRequest(t, srv, http.MethodPost, "/api/fs/projects", `{"type":"directory"}`, "")
		requireStatus(t, resp, http.StatusCreated)

		resp = mustJSONRequest(t, srv, http.MethodPut, "/api/fs/projects/plan.md", `{"content":"# Plan\n\nSynthetic E2E"}`, "")
		requireStatus(t, resp, http.StatusOK)
		var written filePayload
		mustDecodeJSON(t, resp.Body, &written)
		if written.Path != "projects/plan.md" {
			t.Fatalf("path = %q, want %q", written.Path, "projects/plan.md")
		}
		if written.IsJournal {
			t.Fatal("plan.md should not be journal")
		}

		resp = mustRequest(t, srv, http.MethodGet, "/api/fs/projects/plan.md", nil, "", "")
		requireStatus(t, resp, http.StatusOK)
		var file filePayload
		mustDecodeJSON(t, resp.Body, &file)
		if file.Content != "# Plan\n\nSynthetic E2E" {
			t.Fatalf("content = %q, want %q", file.Content, "# Plan\n\nSynthetic E2E")
		}

		resp = mustJSONRequest(t, srv, http.MethodPost, "/api/move/projects/plan.md", `{"destination":"projects/plan-renamed.md"}`, "")
		requireStatus(t, resp, http.StatusOK)

		resp = mustRequest(t, srv, http.MethodGet, "/api/fs/projects/plan.md", nil, "", "")
		requireStatus(t, resp, http.StatusNotFound)

		resp = mustRequest(t, srv, http.MethodGet, "/api/fs/projects", nil, "", "")
		requireStatus(t, resp, http.StatusOK)
		var listed dirPayload
		mustDecodeJSON(t, resp.Body, &listed)
		if !hasEntry(listed.Entries, "plan-renamed.md", false) {
			t.Fatalf("expected plan-renamed.md in listing, got %#v", listed.Entries)
		}

		resp = mustRequest(t, srv, http.MethodDelete, "/api/fs/projects", nil, "", "")
		requireStatus(t, resp, http.StatusBadRequest)

		resp = mustRequest(t, srv, http.MethodDelete, "/api/fs/projects/plan-renamed.md", nil, "", "")
		requireStatus(t, resp, http.StatusOK)

		resp = mustRequest(t, srv, http.MethodDelete, "/api/fs/projects", nil, "", "")
		requireStatus(t, resp, http.StatusOK)
	})

	t.Run("archive lifecycle", func(t *testing.T) {
		resp := mustJSONRequest(t, srv, http.MethodPut, "/api/fs/notes/archive-me.md", `{"content":"v1"}`, "")
		requireStatus(t, resp, http.StatusOK)

		resp = mustRequest(t, srv, http.MethodPost, "/api/archive/notes/archive-me.md", nil, "", "")
		requireStatus(t, resp, http.StatusOK)

		resp = mustRequest(t, srv, http.MethodGet, "/api/fs/notes/archive-me.md", nil, "", "")
		requireStatus(t, resp, http.StatusNotFound)

		resp = mustRequest(t, srv, http.MethodGet, "/api/fs/notes?archived=true", nil, "", "")
		requireStatus(t, resp, http.StatusOK)
		var archivedListing dirPayload
		mustDecodeJSON(t, resp.Body, &archivedListing)
		if !hasEntry(archivedListing.Entries, "archive-me.md", false) {
			t.Fatalf("expected archive-me.md in archived listing, got %#v", archivedListing.Entries)
		}

		resp = mustRequest(t, srv, http.MethodPost, "/api/unarchive/notes/archive-me.md", nil, "", "")
		requireStatus(t, resp, http.StatusOK)

		resp = mustRequest(t, srv, http.MethodGet, "/api/fs/notes/archive-me.md", nil, "", "")
		requireStatus(t, resp, http.StatusOK)
		var restored filePayload
		mustDecodeJSON(t, resp.Body, &restored)
		if restored.Content != "v1" {
			t.Fatalf("content = %q, want %q", restored.Content, "v1")
		}

		resp = mustRequest(t, srv, http.MethodPost, "/api/archive/notes/archive-me.md", nil, "", "")
		requireStatus(t, resp, http.StatusOK)
		resp = mustJSONRequest(t, srv, http.MethodPut, "/api/fs/notes/archive-me.md", `{"content":"current"}`, "")
		requireStatus(t, resp, http.StatusOK)
		resp = mustRequest(t, srv, http.MethodPost, "/api/unarchive/notes/archive-me.md", nil, "", "")
		requireStatus(t, resp, http.StatusConflict)
	})

	t.Run("journal lifecycle", func(t *testing.T) {
		resp := mustJSONRequest(t, srv, http.MethodPost, "/api/journal/self/journal.md", `{"content":"First entry"}`, "")
		requireStatus(t, resp, http.StatusCreated)
		var created filePayload
		mustDecodeJSON(t, resp.Body, &created)
		if !created.IsJournal {
			t.Fatal("journal file must be marked as journal")
		}
		if !strings.Contains(created.Content, "type: journal") {
			t.Fatal("missing journal front matter")
		}

		resp = mustJSONRequest(t, srv, http.MethodPost, "/api/journal/self/journal.md/append", `{"content":"Second entry"}`, "")
		requireStatus(t, resp, http.StatusOK)
		var appended filePayload
		mustDecodeJSON(t, resp.Body, &appended)
		if !strings.Contains(appended.Content, "First entry") || !strings.Contains(appended.Content, "Second entry") {
			t.Fatal("journal content missing expected entries")
		}
		if !strings.Contains(appended.Content, "\n---\n\n## ") {
			t.Fatal("journal append should insert separator")
		}

		resp = mustJSONRequest(t, srv, http.MethodPut, "/api/fs/self/journal.md", `{"content":"overwrite"}`, "")
		requireStatus(t, resp, http.StatusBadRequest)
		var putErr apiError
		mustDecodeJSON(t, resp.Body, &putErr)
		if !strings.Contains(putErr.Error, "journal files must be modified via journal endpoints") {
			t.Fatalf("unexpected error: %q", putErr.Error)
		}

		resp = mustJSONRequest(t, srv, http.MethodPost, "/api/journal/self/journal.md/append", `{"content":""}`, "")
		requireStatus(t, resp, http.StatusBadRequest)
	})

	t.Run("origin guard and CORS", func(t *testing.T) {
		resp := mustJSONRequest(t, srv, http.MethodPut, "/api/fs/security/blocked.md", `{"content":"x"}`, "http://evil.example")
		requireStatus(t, resp, http.StatusForbidden)

		origin := mustOriginFromBaseURL(t, srv.baseURL)
		resp = mustJSONRequest(t, srv, http.MethodPut, "/api/fs/security/allowed.md", `{"content":"ok"}`, origin)
		requireStatus(t, resp, http.StatusOK)

		req, err := http.NewRequest(http.MethodOptions, srv.baseURL+"/api/fs/security/blocked.md", nil)
		if err != nil {
			t.Fatalf("new request: %v", err)
		}
		req.Header.Set("Origin", "http://evil.example")
		resp, err = http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("OPTIONS request failed: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusForbidden {
			t.Fatalf("OPTIONS status = %d, want %d", resp.StatusCode, http.StatusForbidden)
		}
	})

	t.Run("search contract", func(t *testing.T) {
		resp := mustRequest(t, srv, http.MethodGet, "/api/search", nil, "", "")
		requireStatus(t, resp, http.StatusBadRequest)

		resp = mustRequest(t, srv, http.MethodGet, "/api/search?q=hello&sort=recent", nil, "", "")
		requireStatus(t, resp, http.StatusBadRequest)

		resp = mustRequest(t, srv, http.MethodGet, "/api/search?q=hello&limit=0", nil, "", "")
		requireStatus(t, resp, http.StatusBadRequest)

		resp = mustRequest(t, srv, http.MethodGet, "/api/search?q=hello&fields=path,bad", nil, "", "")
		requireStatus(t, resp, http.StatusBadRequest)

		resp = mustRequest(t, srv, http.MethodGet, "/api/search?q=hello", nil, "", "")
		if resp.StatusCode != http.StatusServiceUnavailable && resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			t.Fatalf("search status = %d, want 503 or 200; body=%s", resp.StatusCode, strings.TrimSpace(string(body)))
		}
		if resp.StatusCode == http.StatusServiceUnavailable {
			var e apiError
			mustDecodeJSON(t, resp.Body, &e)
			if !strings.Contains(e.Error, "search is unavailable") {
				t.Fatalf("unexpected search unavailable message: %q", e.Error)
			}
		}
	})

	t.Run("agent script contract", func(t *testing.T) {
		resp := mustRequest(t, srv, http.MethodGet, "/api/agent/version", nil, "", "")
		requireStatus(t, resp, http.StatusOK)
		versionBody, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatalf("read version body: %v", err)
		}
		version := strings.TrimSpace(string(versionBody))
		if _, err := strconv.Atoi(version); err != nil {
			t.Fatalf("version is not numeric: %q", version)
		}

		resp = mustRequest(t, srv, http.MethodGet, "/api/agent/script", nil, "", "")
		requireStatus(t, resp, http.StatusOK)
		scriptBody, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatalf("read script body: %v", err)
		}
		scriptText := string(scriptBody)
		if !strings.Contains(scriptText, "DUFFEL_SCRIPT_VERSION") {
			t.Fatal("agent script missing DUFFEL_SCRIPT_VERSION")
		}
		if !strings.Contains(scriptText, "cmd_search()") {
			t.Fatal("agent script missing search command")
		}

		scriptPath := filepath.Join(t.TempDir(), "duffel.sh")
		if err := os.WriteFile(scriptPath, scriptBody, 0o755); err != nil {
			t.Fatalf("write script: %v", err)
		}

		runCLIExpectOK(t, scriptPath, srv.baseURL, "mkdir", "cli")
		runCLIExpectOK(t, scriptPath, srv.baseURL, "write", "cli/from-cli.md", "hello via cli")
		read := runCLI(t, scriptPath, srv.baseURL, "read", "cli/from-cli.md")
		if read.err != nil {
			t.Fatalf("cli read failed: %v\nstderr:\n%s", read.err, read.stderr)
		}
		if strings.TrimSpace(read.stdout) != "hello via cli" {
			t.Fatalf("cli read stdout = %q, want %q", strings.TrimSpace(read.stdout), "hello via cli")
		}

		runCLIExpectOK(t, scriptPath, srv.baseURL, "mv", "cli/from-cli.md", "cli/moved.md")
		runCLIExpectOK(t, scriptPath, srv.baseURL, "archive", "cli/moved.md")
		runCLIExpectOK(t, scriptPath, srv.baseURL, "unarchive", "cli/moved.md")
		runCLIExpectOK(t, scriptPath, srv.baseURL, "journal", "create", "self/cli-journal.md", "CLI first")
		runCLIExpectOK(t, scriptPath, srv.baseURL, "journal", "append", "self/cli-journal.md", "CLI second")

		resp = mustRequest(t, srv, http.MethodGet, "/api/fs/cli/moved.md", nil, "", "")
		requireStatus(t, resp, http.StatusOK)
		resp = mustRequest(t, srv, http.MethodGet, "/api/fs/self/cli-journal.md", nil, "", "")
		requireStatus(t, resp, http.StatusOK)
		var journal filePayload
		mustDecodeJSON(t, resp.Body, &journal)
		if !strings.Contains(journal.Content, "CLI first") || !strings.Contains(journal.Content, "CLI second") {
			t.Fatalf("journal content missing CLI entries: %q", journal.Content)
		}

		searchNoQuery := runCLI(t, scriptPath, srv.baseURL, "search")
		if searchNoQuery.err == nil {
			t.Fatal("expected `duffel search` without query to fail")
		}
		combined := searchNoQuery.stdout + searchNoQuery.stderr
		if !strings.Contains(combined, "Usage: duffel") || !strings.Contains(combined, "search <query> [options]") {
			t.Fatalf("unexpected output for missing query: stdout=%q stderr=%q", searchNoQuery.stdout, searchNoQuery.stderr)
		}
	})
}

type cliResult struct {
	stdout string
	stderr string
	err    error
}

func runCLI(t *testing.T, scriptPath, baseURL string, args ...string) cliResult {
	t.Helper()
	cmd := exec.Command("bash", append([]string{scriptPath}, args...)...)
	cmd.Env = append(os.Environ(), "DUFFEL_URL="+baseURL)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return cliResult{
		stdout: stdout.String(),
		stderr: stderr.String(),
		err:    err,
	}
}

func runCLIExpectOK(t *testing.T, scriptPath, baseURL string, args ...string) {
	t.Helper()
	res := runCLI(t, scriptPath, baseURL, args...)
	if res.err != nil {
		t.Fatalf("cli command failed: %q\nerror: %v\nstdout:\n%s\nstderr:\n%s", strings.Join(args, " "), res.err, res.stdout, res.stderr)
	}
}

func startSyntheticServer(t *testing.T) *syntheticServer {
	t.Helper()
	repoRoot := findRepoRoot(t)
	port := reservePort(t)
	tmpDir := t.TempDir()
	dataDir := filepath.Join(tmpDir, "data")
	frontendDir := filepath.Join(tmpDir, "frontend")
	homeDir := filepath.Join(tmpDir, "home")
	binPath := filepath.Join(tmpDir, "duffel-e2e-server")

	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatalf("mkdir data dir: %v", err)
	}
	if err := os.MkdirAll(frontendDir, 0o755); err != nil {
		t.Fatalf("mkdir frontend dir: %v", err)
	}
	if err := os.MkdirAll(homeDir, 0o755); err != nil {
		t.Fatalf("mkdir home dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(frontendDir, "index.html"), []byte("<!doctype html><title>duffel-e2e</title>"), 0o644); err != nil {
		t.Fatalf("write frontend index: %v", err)
	}

	build := exec.Command("go", "build", "-o", binPath, "./src/backend/cmd/server")
	build.Dir = repoRoot
	buildOut, err := build.CombinedOutput()
	if err != nil {
		t.Fatalf("build server for e2e: %v\noutput:\n%s", err, string(buildOut))
	}

	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, binPath)
	cmd.Dir = repoRoot
	cmd.Env = append(os.Environ(),
		"DUFFEL_PORT="+strconv.Itoa(port),
		"DUFFEL_DATA_DIR="+dataDir,
		"DUFFEL_FRONTEND_DIR="+frontendDir,
		"HOME="+homeDir,
		"PATH=/usr/bin:/bin:/usr/sbin:/sbin",
		"DUFFEL_ALLOWED_ORIGINS=http://allowed.example",
	)
	srv := &syntheticServer{
		baseURL: fmt.Sprintf("http://127.0.0.1:%d", port),
		cmd:     cmd,
		cancel:  cancel,
	}
	cmd.Stdout = &srv.logs
	cmd.Stderr = &srv.logs

	if err := cmd.Start(); err != nil {
		cancel()
		t.Fatalf("start server: %v", err)
	}

	t.Cleanup(func() {
		cancel()
		done := make(chan error, 1)
		go func() { done <- cmd.Wait() }()
		select {
		case <-time.After(5 * time.Second):
			_ = cmd.Process.Kill()
			<-done
		case <-done:
		}
	})

	waitForServerReady(t, srv)
	return srv
}

func waitForServerReady(t *testing.T, srv *syntheticServer) {
	t.Helper()
	deadline := time.Now().Add(45 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get(srv.baseURL + "/api/agent/version")
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("server did not become ready at %s\nlogs:\n%s", srv.baseURL, srv.logs.String())
}

func findRepoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}

	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not find repo root (go.mod)")
		}
		dir = parent
	}
}

func reservePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve port: %v", err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

func mustJSONRequest(t *testing.T, srv *syntheticServer, method, path, jsonBody, origin string) *http.Response {
	t.Helper()
	return mustRequest(t, srv, method, path, strings.NewReader(jsonBody), "application/json", origin)
}

func mustRequest(t *testing.T, srv *syntheticServer, method, path string, body io.Reader, contentType string, origin string) *http.Response {
	t.Helper()
	targetURL := srv.baseURL + path
	req, err := http.NewRequest(method, targetURL, body)
	if err != nil {
		t.Fatalf("new request %s %s: %v", method, path, err)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request %s %s failed: %v\nserver logs:\n%s", method, path, err, srv.logs.String())
	}
	t.Cleanup(func() {
		io.Copy(io.Discard, resp.Body) //nolint:errcheck
		resp.Body.Close()
	})
	return resp
}

func requireStatus(t *testing.T, resp *http.Response, want int) {
	t.Helper()
	if resp.StatusCode == want {
		return
	}
	body, _ := io.ReadAll(resp.Body)
	t.Fatalf("status = %d, want %d; body=%s", resp.StatusCode, want, strings.TrimSpace(string(body)))
}

func mustDecodeJSON(t *testing.T, r io.Reader, out any) {
	t.Helper()
	if err := json.NewDecoder(r).Decode(out); err != nil {
		t.Fatalf("decode JSON: %v", err)
	}
}

func hasEntry(entries []entryPayload, name string, isDir bool) bool {
	for _, e := range entries {
		if e.Name == name && e.IsDir == isDir {
			return true
		}
	}
	return false
}

func mustOriginFromBaseURL(t *testing.T, baseURL string) string {
	t.Helper()
	u, err := url.Parse(baseURL)
	if err != nil {
		t.Fatalf("parse base URL: %v", err)
	}
	return u.Scheme + "://" + u.Host
}
