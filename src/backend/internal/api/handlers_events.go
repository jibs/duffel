package api

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type eventBroker struct {
	mu          sync.Mutex
	nextID      uint64
	subscribers map[chan uint64]struct{}
}

func newEventBroker() *eventBroker {
	return &eventBroker{subscribers: make(map[chan uint64]struct{})}
}

func (b *eventBroker) Broadcast() {
	b.mu.Lock()
	b.nextID++
	id := b.nextID
	for ch := range b.subscribers {
		select {
		case ch <- id:
		default:
		}
	}
	b.mu.Unlock()
}

func (b *eventBroker) subscribe() (chan uint64, func()) {
	ch := make(chan uint64, 1)
	b.mu.Lock()
	b.subscribers[ch] = struct{}{}
	b.mu.Unlock()

	unsubscribe := func() {
		b.mu.Lock()
		delete(b.subscribers, ch)
		close(ch)
		b.mu.Unlock()
	}
	return ch, unsubscribe
}

func handleEvents(broker *eventBroker) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			writeError(w, http.StatusInternalServerError, "streaming not supported", r.URL.Path)
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("X-Accel-Buffering", "no")

		events, unsubscribe := broker.subscribe()
		defer unsubscribe()

		if !writeSSE(w, flusher, ": connected\n\n") {
			return
		}

		heartbeat := time.NewTicker(30 * time.Second)
		defer heartbeat.Stop()

		for {
			select {
			case id := <-events:
				if !writeSSE(w, flusher, "id: %d\nevent: content-changed\ndata: {\"version\":%d}\n\n", id, id) {
					return
				}
			case <-heartbeat.C:
				if !writeSSE(w, flusher, ": heartbeat\n\n") {
					return
				}
			case <-r.Context().Done():
				return
			}
		}
	}
}

func writeSSE(w http.ResponseWriter, flusher http.Flusher, format string, args ...any) bool {
	if _, err := fmt.Fprintf(w, format, args...); err != nil {
		return false
	}
	flusher.Flush()
	return true
}

func startFilesystemChangeWatcher(root string, onContentChanged func()) {
	previous, err := filesystemSnapshot(root)
	if err != nil {
		previous = map[string]string{}
	}

	go func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()

		for range ticker.C {
			current, err := filesystemSnapshot(root)
			if err != nil {
				continue
			}
			if !sameSnapshot(previous, current) {
				previous = current
				onContentChanged()
			}
		}
	}()
}

func filesystemSnapshot(root string) (map[string]string, error) {
	snapshot := make(map[string]string)
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		snapshot[rel] = fmt.Sprintf("%t:%d:%d", entry.IsDir(), info.Size(), info.ModTime().UnixNano())
		return nil
	})
	return snapshot, err
}

func sameSnapshot(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for key, value := range a {
		if b[key] != value {
			return false
		}
	}
	return true
}
