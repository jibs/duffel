package main

import (
	"fmt"
	"log"
	"net"
	"net/http"
	"sync/atomic"

	"duffel/src/backend/internal/api"
	"duffel/src/backend/internal/config"
	"duffel/src/backend/internal/search"
	"duffel/src/backend/internal/storage"
)

func localIP() string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return ""
	}
	for _, a := range addrs {
		if ipnet, ok := a.(*net.IPNet); ok && !ipnet.IP.IsLoopback() && ipnet.IP.To4() != nil {
			return ipnet.IP.String()
		}
	}
	return ""
}

func main() {
	cfg := config.Load()

	store, err := storage.NewStore(cfg.DataDir)
	if err != nil {
		log.Fatalf("Failed to initialize storage: %v", err)
	}

	if err := search.EnsureCollection("duffel", cfg.DataDir); err != nil {
		log.Printf("qmd: could not configure collection: %v", err)
		log.Printf("qmd: run 'pnpm install' to provision vendored qmd, then restart the server to enable search")
	}

	var searcherPtr atomic.Pointer[search.Searcher]

	// Try to open searcher immediately (DB may exist from a previous run)
	if s, err := search.NewSearcher(); err == nil {
		log.Printf("qmd: search enabled (using %s)", "~/.cache/qmd/index.sqlite")
		searcherPtr.Store(s)
	} else {
		log.Printf("qmd: search not yet available: %v", err)
	}

	// Start background indexing; when done, open/reopen the searcher
	if err := search.StartIndexing("duffel", func(err error) {
		if err != nil {
			log.Printf("qmd: indexing failed: %v", err)
			return
		}
		log.Printf("qmd: indexing complete")
		s, err := search.NewSearcher()
		if err != nil {
			log.Printf("qmd: failed to open searcher after indexing: %v", err)
			return
		}
		// Close old searcher if any
		if old := searcherPtr.Swap(s); old != nil {
			_ = old.Close()
		}
		log.Printf("qmd: search enabled (using %s)", "~/.cache/qmd/index.sqlite")
	}); err != nil {
		log.Printf("qmd: background indexing not started: %v", err)
	} else {
		log.Printf("qmd: background indexing started")
	}

	getSearcher := func() *search.Searcher { return searcherPtr.Load() }
	router := api.NewRouter(store, getSearcher, cfg.FrontendDir)

	addr := fmt.Sprintf(":%s", cfg.Port)
	log.Printf("duffel starting (data: %s, frontend: %s)", cfg.DataDir, cfg.FrontendDir)
	log.Printf("  http://localhost:%s", cfg.Port)
	if ip := localIP(); ip != "" {
		log.Printf("  http://%s:%s", ip, cfg.Port)
	}
	if err := http.ListenAndServe(addr, router); err != nil {
		log.Fatalf("Server error: %v", err)
	}
}
