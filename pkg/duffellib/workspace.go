// Package duffellib provides the filesystem-backed knowledge workspace
// primitives used by Duffel and by other Go projects.
package duffellib

import (
	"bytes"
	"errors"
	"fmt"
	"sync"

	"duffel/pkg/duffellib/markdown"
	"duffel/pkg/duffellib/search"
	"duffel/pkg/duffellib/storage"
)

const defaultCollection = "duffel"

// ErrVerificationFailed is returned when a verified write cannot be read back
// as the same content.
var ErrVerificationFailed = errors.New("document verification failed")

// Options configures a Workspace.
type Options struct {
	// Root is the on-disk directory that contains workspace documents.
	Root string
	// Collection names the search collection for this workspace. If empty,
	// "duffel" is used.
	Collection string
	// EnableSearch registers the root with the search backend during Open.
	EnableSearch bool
}

// Workspace is a safe filesystem-backed knowledge workspace with first-class
// document search.
type Workspace struct {
	store      *storage.Store
	collection string

	mu       sync.RWMutex
	searcher *search.Searcher
}

// VerificationReport describes the result of verifying a document.
type VerificationReport struct {
	Path      string `json:"path"`
	Size      int64  `json:"size"`
	IsJournal bool   `json:"isJournal"`
	Kind      string `json:"kind"`
	Valid     bool   `json:"valid"`
}

// Open creates a Workspace rooted at opts.Root.
func Open(opts Options) (*Workspace, error) {
	if opts.Root == "" {
		return nil, fmt.Errorf("root is required")
	}
	collection := opts.Collection
	if collection == "" {
		collection = defaultCollection
	}
	store, err := storage.NewStore(opts.Root)
	if err != nil {
		return nil, err
	}
	ws := &Workspace{store: store, collection: collection}
	if opts.EnableSearch {
		if err := ws.EnsureSearch(); err != nil {
			return nil, err
		}
	}
	return ws, nil
}

// Store returns the lower-level storage API for callers that need direct file
// operations.
func (w *Workspace) Store() *storage.Store {
	return w.store
}

// Root returns the absolute on-disk workspace root.
func (w *Workspace) Root() string {
	return w.store.Root()
}

// Collection returns the search collection name used by this workspace.
func (w *Workspace) Collection() string {
	return w.collection
}

// EnsureSearch registers the workspace root with the search backend.
func (w *Workspace) EnsureSearch() error {
	return search.EnsureCollection(w.collection, w.store.Root())
}

// RefreshSearch starts an asynchronous search index refresh.
func (w *Workspace) RefreshSearch(onDone func(error)) error {
	return search.StartIndexing(w.collection, onDone)
}

// ReloadSearcher refreshes the query handle used by Search.
func (w *Workspace) ReloadSearcher() error {
	searcher, err := search.NewSearcher()
	if err != nil {
		return err
	}
	w.mu.Lock()
	old := w.searcher
	w.searcher = searcher
	w.mu.Unlock()
	if old != nil {
		_ = old.Close()
	}
	return nil
}

// Search queries the workspace. If opts.Collection is empty, the workspace
// collection is used. Result paths are mapped relative to the workspace root.
func (w *Workspace) Search(opts search.SearchOptions) ([]search.Result, error) {
	if opts.Collection == "" {
		opts.Collection = w.collection
	}

	searcher := w.currentSearcher()
	if searcher == nil {
		if err := w.ReloadSearcher(); err != nil {
			return nil, err
		}
		searcher = w.currentSearcher()
	}

	results, err := searcher.Search(opts)
	if err != nil {
		return nil, err
	}
	return search.MapPaths(results, w.store.Root()), nil
}

func (w *Workspace) currentSearcher() *search.Searcher {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.searcher
}

// Close releases resources held by the workspace.
func (w *Workspace) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.searcher == nil {
		return nil
	}
	err := w.searcher.Close()
	w.searcher = nil
	return err
}

// List returns directory contents.
func (w *Workspace) List(path string, includeArchived bool) (*storage.DirInfo, error) {
	return w.store.List(path, includeArchived)
}

// Read reads a text document.
func (w *Workspace) Read(path string) (*storage.FileInfo, error) {
	return w.store.Read(path)
}

// Write writes a validated text document.
func (w *Workspace) Write(path string, content []byte) error {
	return w.store.Write(path, content)
}

// WriteVerified writes a validated text document, reads it back, and verifies
// the persisted content matches.
func (w *Workspace) WriteVerified(path string, content []byte) (*VerificationReport, error) {
	if err := w.Write(path, content); err != nil {
		return nil, err
	}
	file, err := w.Read(path)
	if err != nil {
		return nil, err
	}
	report, err := verifyFile(file)
	if err != nil {
		return report, err
	}
	if !bytes.Equal([]byte(file.Content), content) {
		report.Valid = false
		return report, ErrVerificationFailed
	}
	return report, nil
}

// Verify validates an existing text document and returns its metadata.
func (w *Workspace) Verify(path string) (*VerificationReport, error) {
	file, err := w.Read(path)
	if err != nil {
		return nil, err
	}
	return verifyFile(file)
}

func verifyFile(file *storage.FileInfo) (*VerificationReport, error) {
	report := &VerificationReport{
		Path:      file.Path,
		Size:      file.Size,
		IsJournal: file.IsJournal,
		Kind:      file.Kind,
	}
	if err := markdown.Validate([]byte(file.Content)); err != nil {
		return report, err
	}
	report.Valid = true
	return report, nil
}

// WriteRaw writes allowlisted binary content, such as images.
func (w *Workspace) WriteRaw(path string, content []byte) error {
	return w.store.WriteRaw(path, content)
}

// CreateJournal creates a journal document.
func (w *Workspace) CreateJournal(path string, initialContent string) error {
	return w.store.CreateJournal(path, initialContent)
}

// AppendJournal appends a timestamped journal entry.
func (w *Workspace) AppendJournal(path string, entryContent string) error {
	return w.store.AppendJournal(path, entryContent)
}

// Archive moves a file into its sibling .archive directory.
func (w *Workspace) Archive(path string) error {
	return w.store.Archive(path)
}

// Unarchive restores a file from its sibling .archive directory.
func (w *Workspace) Unarchive(path string) error {
	return w.store.Unarchive(path)
}
