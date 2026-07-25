package api_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestIllustrationAPIPersistsAndProtectsConcurrentChanges(t *testing.T) {
	srv, store := setupTestServer(t)
	defer srv.Close()
	if err := store.Write("notes/plan.md", []byte("# Plan")); err != nil {
		t.Fatal(err)
	}

	resp, err := http.Get(srv.URL + "/api/illustrations/notes/plan.md")
	if err != nil {
		t.Fatal(err)
	}
	var empty struct {
		Exists bool `json:"exists"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&empty); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if empty.Exists {
		t.Fatal("new note unexpectedly has an illustration")
	}

	req, _ := http.NewRequest(http.MethodPut, srv.URL+"/api/illustrations/notes/plan.md",
		strings.NewReader(`{"version":1,"mermaid":"flowchart LR\n  Human --> Duffel"}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var created struct {
		Exists   bool   `json:"exists"`
		Revision string `json:"revision"`
		Document struct {
			Mermaid string `json:"mermaid"`
		} `json:"document"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || !created.Exists || created.Revision == "" {
		t.Fatalf("create status=%d response=%#v", resp.StatusCode, created)
	}
	if created.Document.Mermaid != "flowchart LR\n  Human --> Duffel" {
		t.Fatalf("Mermaid = %q", created.Document.Mermaid)
	}

	noteUpdate, _ := http.NewRequest(http.MethodPut, srv.URL+"/api/fs/notes/plan.md",
		strings.NewReader(`{"content":"# Updated plan"}`))
	noteUpdate.Header.Set("Content-Type", "application/json")
	noteUpdateResp, err := http.DefaultClient.Do(noteUpdate)
	if err != nil {
		t.Fatal(err)
	}
	noteUpdateResp.Body.Close()
	if noteUpdateResp.StatusCode != http.StatusOK {
		t.Fatalf("note update status = %d", noteUpdateResp.StatusCode)
	}
	stillAttached, err := http.Get(srv.URL + "/api/illustrations/notes/plan.md")
	if err != nil {
		t.Fatal(err)
	}
	var afterNoteEdit struct {
		Exists bool `json:"exists"`
	}
	if err := json.NewDecoder(stillAttached.Body).Decode(&afterNoteEdit); err != nil {
		t.Fatal(err)
	}
	stillAttached.Body.Close()
	if !afterNoteEdit.Exists {
		t.Fatal("editing the note removed its illustration")
	}

	conflictReq, _ := http.NewRequest(http.MethodPut, srv.URL+"/api/illustrations/notes/plan.md",
		strings.NewReader(`{"version":1,"mermaid":"flowchart LR\n  Agent --> Duffel"}`))
	conflictReq.Header.Set("Content-Type", "application/json")
	conflictReq.Header.Set("If-Match", "stale-revision")
	conflictResp, err := http.DefaultClient.Do(conflictReq)
	if err != nil {
		t.Fatal(err)
	}
	conflictResp.Body.Close()
	if conflictResp.StatusCode != http.StatusConflict {
		t.Fatalf("conflict status = %d, want 409", conflictResp.StatusCode)
	}

	root, err := store.List("", false)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range root.Entries {
		if entry.Name == ".duffel" {
			t.Fatal("hidden illustration storage leaked into workspace listing")
		}
	}
}

func TestMovingAndDeletingNoteCarriesIllustration(t *testing.T) {
	srv, store := setupTestServer(t)
	defer srv.Close()
	if err := store.Write("notes/plan.md", []byte("# Plan")); err != nil {
		t.Fatal(err)
	}

	req, _ := http.NewRequest(http.MethodPut, srv.URL+"/api/illustrations/notes/plan.md",
		strings.NewReader(`{"version":1,"mermaid":"flowchart LR\n  A --> B"}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	resp, err = http.Post(srv.URL+"/api/move/notes/plan.md", "application/json",
		strings.NewReader(`{"destination":"notes/moved.md"}`))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("move status = %d", resp.StatusCode)
	}

	resp, err = http.Get(srv.URL + "/api/illustrations/notes/moved.md")
	if err != nil {
		t.Fatal(err)
	}
	var moved struct {
		Exists bool `json:"exists"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&moved); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if !moved.Exists {
		t.Fatal("illustration did not move with note")
	}

	deleteReq, _ := http.NewRequest(http.MethodDelete, srv.URL+"/api/fs/notes/moved.md", nil)
	resp, err = http.DefaultClient.Do(deleteReq)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("delete status = %d", resp.StatusCode)
	}
	if _, err := store.Read(".duffel/illustrations/notes/moved.md.json"); err == nil {
		t.Fatal("illustration sidecar remains after deleting note")
	}
}
