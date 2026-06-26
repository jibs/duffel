package main

import (
	"fmt"
	"log"
	"net"
	"net/http"
	"sync/atomic"

	"duffel/pkg/duffellib"
	"duffel/pkg/duffellib/search"
	"duffel/src/backend/internal/api"
	"duffel/src/backend/internal/config"
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

	workspace, err := duffellib.Open(duffellib.Options{
		Root:         cfg.DataDir,
		Collection:   "duffel",
		EnableSearch: true,
	})
	if err != nil {
		log.Printf("search: could not configure collection: %v", err)
		log.Printf("search: continuing without initial search setup; run 'pnpm install' to provision search dependencies, then restart the server to enable search")
		workspace, err = duffellib.Open(duffellib.Options{
			Root:       cfg.DataDir,
			Collection: "duffel",
		})
		if err != nil {
			log.Fatalf("Failed to initialize workspace: %v", err)
		}
	}
	store := workspace.Store()

	var searcherPtr atomic.Pointer[search.Searcher]

	// Try to open searcher immediately (DB may exist from a previous run)
	if s, err := search.NewSearcher(); err == nil {
		log.Printf("search: enabled")
		searcherPtr.Store(s)
	} else {
		log.Printf("search: not yet available: %v", err)
	}

	// Start background indexing; when done, open/reopen the searcher
	reindexer := search.NewReindexScheduler(workspace.Collection(), func(err error) {
		if err != nil {
			log.Printf("search: indexing failed: %v", err)
			return
		}
		log.Printf("search: indexing complete")
		s, err := search.NewSearcher()
		if err != nil {
			log.Printf("search: failed to open searcher after indexing: %v", err)
			return
		}
		// Close old searcher if any
		if old := searcherPtr.Swap(s); old != nil {
			_ = old.Close()
		}
		log.Printf("search: enabled")
	})
	if err := reindexer.Trigger(); err != nil {
		log.Printf("search: background indexing not started: %v", err)
	} else {
		log.Printf("search: background indexing started")
	}

	getSearcher := func() *search.Searcher { return searcherPtr.Load() }
	onContentChanged := func() {
		if err := reindexer.Trigger(); err != nil {
			log.Printf("search: background indexing not started after content mutation: %v", err)
		}
	}
	router := api.NewRouter(store, getSearcher, onContentChanged, cfg.FrontendDir)

	addr := fmt.Sprintf("%s:%s", cfg.Host, cfg.Port)
	log.Printf("duffel starting (data: %s, frontend: %s)", cfg.DataDir, cfg.FrontendDir)
	if cfg.Host != "" {
		log.Printf("  http://%s:%s (bound to %s)", cfg.Host, cfg.Port, cfg.Host)
	} else {
		log.Printf("  http://localhost:%s", cfg.Port)
		if ip := localIP(); ip != "" {
			log.Printf("  http://%s:%s", ip, cfg.Port)
		}
	}
	log.Printf("  MCP endpoint: http://%s:%s/mcp (streamable-http)", func() string {
		if cfg.Host != "" {
			return cfg.Host
		}
		return "localhost"
	}(), cfg.Port)
	if err := http.ListenAndServe(addr, router); err != nil {
		log.Fatalf("Server error: %v", err)
	}
}
