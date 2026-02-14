package config

import (
	"os"
	"path/filepath"
)

type Config struct {
	Port        string
	DataDir     string
	FrontendDir string
}

func Load() Config {
	c := Config{
		Port:        envOr("DUFFEL_PORT", "4386"),
		DataDir:     envOr("DUFFEL_DATA_DIR", "./data"),
		FrontendDir: envOr("DUFFEL_FRONTEND_DIR", "./src/frontend"),
	}
	c.DataDir, _ = filepath.Abs(c.DataDir)
	c.FrontendDir, _ = filepath.Abs(c.FrontendDir)
	return c
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
