package api_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"testing"
)

// pngBytes is a minimal PNG header plus binary payload (includes a null byte).
var pngBytes = []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0x00, 0x01, 0x02, 0x03}

func putImage(t *testing.T, url, contentType string, body []byte) *http.Response {
	t.Helper()
	req, _ := http.NewRequest("PUT", url, bytes.NewReader(body))
	req.Header.Set("Content-Type", contentType)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func TestImageUploadAndRawServe(t *testing.T) {
	srv, _ := setupTestServer(t)
	defer srv.Close()

	resp := putImage(t, srv.URL+"/api/fs/_attachments/pic.png", "image/png", pngBytes)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("PUT image status = %d, want 200", resp.StatusCode)
	}

	// Raw GET returns the exact bytes with the allowlisted Content-Type.
	raw, err := http.Get(srv.URL + "/api/fs/_attachments/pic.png?raw=1")
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Body.Close()
	if raw.StatusCode != http.StatusOK {
		t.Fatalf("raw GET status = %d, want 200", raw.StatusCode)
	}
	if ct := raw.Header.Get("Content-Type"); ct != "image/png" {
		t.Errorf("raw Content-Type = %q, want image/png", ct)
	}
	got := new(bytes.Buffer)
	got.ReadFrom(raw.Body)
	if !bytes.Equal(got.Bytes(), pngBytes) {
		t.Errorf("raw bytes differ from upload")
	}
}

func TestImageNonRawGetReturnsMetadata(t *testing.T) {
	srv, _ := setupTestServer(t)
	defer srv.Close()

	putImage(t, srv.URL+"/api/fs/_attachments/pic.png", "image/png", pngBytes).Body.Close()

	resp, err := http.Get(srv.URL + "/api/fs/_attachments/pic.png")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET status = %d, want 200", resp.StatusCode)
	}
	var payload map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if payload["kind"] != "image" {
		t.Errorf("kind = %v, want image", payload["kind"])
	}
	if payload["content"] != "" {
		t.Errorf("content = %q, want empty for image", payload["content"])
	}
}

func TestImageRawRangeRequest(t *testing.T) {
	srv, _ := setupTestServer(t)
	defer srv.Close()

	putImage(t, srv.URL+"/api/fs/_attachments/pic.png", "image/png", pngBytes).Body.Close()

	req, _ := http.NewRequest("GET", srv.URL+"/api/fs/_attachments/pic.png?raw=1", nil)
	req.Header.Set("Range", "bytes=0-3")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusPartialContent {
		t.Fatalf("Range GET status = %d, want 206", resp.StatusCode)
	}
	body := new(bytes.Buffer)
	body.ReadFrom(resp.Body)
	if !bytes.Equal(body.Bytes(), pngBytes[0:4]) {
		t.Errorf("range body = %v, want %v", body.Bytes(), pngBytes[0:4])
	}
}

func TestImageUploadRejectsNonImageExtension(t *testing.T) {
	srv, _ := setupTestServer(t)
	defer srv.Close()

	resp := putImage(t, srv.URL+"/api/fs/notes/evil.md", "image/png", pngBytes)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

func TestImageUploadTooLarge(t *testing.T) {
	srv, _ := setupTestServer(t)
	defer srv.Close()

	big := bytes.Repeat([]byte{0x01}, (25<<20)+1024)
	resp := putImage(t, srv.URL+"/api/fs/_attachments/big.png", "image/png", big)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413", resp.StatusCode)
	}
}

func TestSVGRawServeIsHardened(t *testing.T) {
	srv, _ := setupTestServer(t)
	defer srv.Close()

	svg := []byte(`<svg xmlns="http://www.w3.org/2000/svg"></svg>`)
	putImage(t, srv.URL+"/api/fs/_attachments/art.svg", "image/svg+xml", svg).Body.Close()

	resp, err := http.Get(srv.URL + "/api/fs/_attachments/art.svg?raw=1")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if ct := resp.Header.Get("Content-Type"); ct != "image/svg+xml" {
		t.Errorf("Content-Type = %q, want image/svg+xml", ct)
	}
	if csp := resp.Header.Get("Content-Security-Policy"); csp == "" {
		t.Error("expected a Content-Security-Policy header on SVG")
	}
	if cd := resp.Header.Get("Content-Disposition"); cd != "attachment" {
		t.Errorf("Content-Disposition = %q, want attachment", cd)
	}
}
