package storage

import (
	"bytes"
	"errors"
	"os"
	"testing"
)

// pngBytes is a minimal PNG header plus a null byte — definitively binary.
var pngBytes = []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0x00, 0x01, 0x02, 0x03}

func TestFileKind_Image(t *testing.T) {
	cases := map[string]string{
		"photo.png":      "image",
		"photo.PNG":      "image",
		"scan.jpg":       "image",
		"scan.jpeg":      "image",
		"anim.gif":       "image",
		"pic.webp":       "image",
		"pic.avif":       "image",
		"icon.ico":       "image",
		"art.svg":        "image",
		"notes.md":       "markdown",
		"page.html":      "html",
		"data.txt":       "text",
		"_attachments/x": "text",
	}
	for path, want := range cases {
		if got := fileKind(path, false, false); got != want {
			t.Errorf("fileKind(%q) = %q, want %q", path, got, want)
		}
	}
}

func TestWriteRaw_RoundTrip(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	if err := store.WriteRaw("_attachments/pic.png", pngBytes); err != nil {
		t.Fatalf("WriteRaw: %v", err)
	}

	fsPath, err := store.RawPath("_attachments/pic.png")
	if err != nil {
		t.Fatalf("RawPath: %v", err)
	}
	got, err := os.ReadFile(fsPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, pngBytes) {
		t.Errorf("round-tripped bytes differ: got %v, want %v", got, pngBytes)
	}
}

func TestWriteRaw_RejectsNonImageExtension(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	for _, path := range []string{"notes.md", "data.txt", "page.html"} {
		if err := store.WriteRaw(path, pngBytes); !errors.Is(err, ErrUnsupportedBinary) {
			t.Errorf("WriteRaw(%q) err = %v, want ErrUnsupportedBinary", path, err)
		}
		if _, statErr := store.RawPath(path); !errors.Is(statErr, ErrNotFound) {
			t.Errorf("WriteRaw(%q) should not have created a file (RawPath err = %v)", path, statErr)
		}
	}
}

func TestWriteRaw_RejectsJournalOverwrite(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.CreateJournal("log.png", "entry"); err != nil {
		t.Fatal(err)
	}
	if err := store.WriteRaw("log.png", pngBytes); !errors.Is(err, ErrIsJournal) {
		t.Errorf("WriteRaw over journal err = %v, want ErrIsJournal", err)
	}
}

func TestWrite_StillRejectsBinary(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	// The text path must keep rejecting binary even for an image extension.
	if err := store.Write("pic.png", pngBytes); err == nil {
		t.Error("Write should reject binary content")
	}
}

func TestStatFile_ImageHasNoContent(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.WriteRaw("_attachments/pic.png", pngBytes); err != nil {
		t.Fatal(err)
	}
	info, err := store.StatFile("_attachments/pic.png")
	if err != nil {
		t.Fatalf("StatFile: %v", err)
	}
	if info.Content != "" {
		t.Errorf("StatFile Content = %q, want empty", info.Content)
	}
	if info.Kind != "image" {
		t.Errorf("StatFile Kind = %q, want image", info.Kind)
	}
	if info.Size != int64(len(pngBytes)) {
		t.Errorf("StatFile Size = %d, want %d", info.Size, len(pngBytes))
	}
}
