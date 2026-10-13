package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"duffel/pkg/duffellib/storage"
	"github.com/go-chi/chi/v5"
)

const validIllustrationSnapshot = `{"document":{"schema":{"schemaVersion":2,"sequences":{"com.tldraw.store":5}},"store":{"document:document":{"id":"document:document","typeName":"document","name":"","gridSize":10,"meta":{}},"page:page":{"id":"page:page","typeName":"page","name":"Page 1","index":"a1","meta":{}},"shape:frame":{"id":"shape:frame","typeName":"shape","type":"frame","x":0,"y":0,"rotation":0,"opacity":1,"isLocked":false,"index":"a1","parentId":"page:page","meta":{},"props":{"w":100,"h":100,"name":"Frame","color":"black"}}}}}`

func TestIllustrationRejectsMalformedSnapshots(t *testing.T) {
	for _, raw := range []string{
		`null`, `7`, `[]`, `{}`, `{"document":{}}`,
		strings.Replace(validIllustrationSnapshot, `"schemaVersion":2`, `"schemaVersion":99`, 1),
		strings.Replace(validIllustrationSnapshot, `"x":0`, `"x":"zero"`, 1),
		strings.Replace(validIllustrationSnapshot, `"w":100`, `"w":[]`, 1),
		strings.Replace(validIllustrationSnapshot, `"color":"black"`, `"color":"unknown"`, 1),
		strings.Replace(validIllustrationSnapshot, `"com.tldraw.store":5`, `"com.tldraw.store":999`, 1),
		strings.Replace(validIllustrationSnapshot, `"com.tldraw.store":5`, `"com.tldraw.foreign":1`, 1),
		strings.Replace(validIllustrationSnapshot, `"parentId":"page:page"`, `"parentId":"shape:frame"`, 1),
		strings.Replace(validIllustrationSnapshot, `"parentId":"page:page"`, `"parentId":"shape:missing"`, 1),
		strings.Replace(validIllustrationSnapshot, `"type":"frame"`, `"type":"unknown"`, 1),
	} {
		if err := validateIllustrationDocument(&illustrationDocument{Version: 1, Snapshot: json.RawMessage(raw)}); err == nil {
			t.Errorf("accepted malformed snapshot: %s", raw)
		}
	}
	if err := validateIllustrationSnapshot(json.RawMessage(validIllustrationSnapshot)); err != nil {
		t.Fatal(err)
	}
}

func TestIllustrationAssetPolicy(t *testing.T) {
	for _, src := range []string{
		"https://attacker.example/track.png", "//attacker.example/track.png", "http://127.0.0.1/private.png",
		"javascript:alert(1)", "blob:remote", "data:image/svg+xml;base64,PHN2Zz4=", "data:text/html;base64,PHNjcmlwdD4=",
		"data:image/png;base64,PHNjcmlwdD4=", "/api/fs/../secret.png?raw=1", "/api/fs/pic.svg?raw=1",
		"/api/fs/pic.png?raw=1&extra=1", "/api/fs/pic.png", "/oauth/authorize",
	} {
		if err := validateIllustrationAssetURL(src); err == nil {
			t.Errorf("accepted unsafe asset %s", src)
		}
	}
	for _, src := range []string{"", "/api/fs/images/pic.png?raw=1", "data:image/png;base64,iVBORw0KGgo="} {
		if err := validateIllustrationAssetURL(src); err != nil {
			t.Errorf("rejected safe asset %s: %v", src, err)
		}
	}
	// Policy applies to the actual persisted snapshot, including hidden assets.
	raw := strings.Replace(validIllustrationSnapshot, `"meta":{}`, `"meta":{"src":"https://attacker.example/track.png"}`, 1)
	if err := validateIllustrationSnapshot(json.RawMessage(raw)); err == nil {
		t.Fatal("remote asset accepted in snapshot")
	}
}

func TestIllustrationConcurrentCASAcrossStores(t *testing.T) {
	root := t.TempDir()
	store, err := storage.NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Write("note.md", []byte("# Note")); err != nil {
		t.Fatal(err)
	}
	for _, initial := range []string{"none", "existing"} {
		t.Run(initial, func(t *testing.T) {
			revision := "none"
			if initial == "existing" {
				record, err := writeIllustration(store, "note.md", illustrationDocument{Version: 1, Mermaid: "flowchart LR\n A --> B"}, "")
				if err != nil {
					t.Fatal(err)
				}
				revision = record.Revision
			}
			const writers = 24
			start := make(chan struct{})
			results := make(chan error, writers)
			var wg sync.WaitGroup
			for i := 0; i < writers; i++ {
				other, err := storage.NewStore(root)
				if err != nil {
					t.Fatal(err)
				}
				wg.Add(1)
				go func() {
					defer wg.Done()
					<-start
					document := illustrationDocument{Version: 1, Mermaid: fmt.Sprintf("flowchart LR\n A --> Writer%d", i)}
					record, err := writeIllustration(other, "note.md", document, revision)
					if err == nil && record.Revision != illustrationRevision(string(mustMarshalIllustration(document))) {
						err = errors.New("response returned another writer's revision")
					}
					results <- err
				}()
			}
			close(start)
			wg.Wait()
			close(results)
			successes := 0
			for err := range results {
				if err == nil {
					successes++
				} else if !errors.Is(err, errIllustrationConflict) {
					t.Fatal(err)
				}
			}
			if successes != 1 {
				t.Fatalf("%d writers succeeded; want exactly 1", successes)
			}
		})
	}
}
func mustMarshalIllustration(document illustrationDocument) []byte {
	data, _ := json.Marshal(document)
	return data
}

func TestIllustrationRejectsExtraJSON(t *testing.T) {
	store, err := storage.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Write("note.md", []byte("# Note")); err != nil {
		t.Fatal(err)
	}
	router := chi.NewRouter()
	router.Put("/api/illustrations/*", handleIllustration(store, nil))
	for _, body := range []string{`{"version":1,"mermaid":"flowchart LR\n A-->B"} {}`, `{"version":1,"mermaid":"flowchart LR\n A-->B","extra":true}`} {
		request := httptest.NewRequest(http.MethodPut, "/api/illustrations/note.md", strings.NewReader(body))
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != http.StatusBadRequest {
			t.Fatalf("status=%d body=%s", response.Code, response.Body)
		}
	}
	request := httptest.NewRequest(http.MethodPut, "/api/illustrations/note.md", strings.NewReader(`{"version":1,"mermaid":"flowchart LR\n A-->B"}`))
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("valid body status=%d body=%s", response.Code, response.Body)
	}
}

func TestStaticCachePolicy(t *testing.T) {
	for path, policy := range map[string]string{
		"/illustration.html":                     "public, max-age=0, must-revalidate",
		"/js/drawing/illustration.js":            "public, max-age=0, must-revalidate",
		"/js/drawing/illustration.css":           "public, max-age=0, must-revalidate",
		"/js/drawing/chunks/chunk-ABC12345.js":   "public, max-age=31536000, immutable",
		"/js/drawing/assets/font-ABC12345.woff2": "public, max-age=31536000, immutable",
	} {
		recorder := httptest.NewRecorder()
		noCacheMiddleware(staticCacheMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }))).ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
		if got := recorder.Header().Get("Cache-Control"); got != policy {
			t.Errorf("%s cache = %q", path, got)
		}
		if recorder.Header().Get("Pragma") != "" || recorder.Header().Get("Expires") != "" {
			t.Error("stale no-cache headers on static asset")
		}
	}
}
