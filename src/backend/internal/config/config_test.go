package config

import (
	"path/filepath"
	"testing"
)

func TestLoad_Defaults(t *testing.T) {
	// Clear env vars to ensure defaults
	t.Setenv("DUFFEL_PORT", "")
	t.Setenv("DUFFEL_DATA_DIR", "")
	t.Setenv("DUFFEL_FRONTEND_DIR", "")

	c := Load()

	if c.Port != "4386" {
		t.Errorf("Port = %q, want %q", c.Port, "4386")
	}

	if !filepath.IsAbs(c.DataDir) {
		t.Errorf("DataDir %q is not absolute", c.DataDir)
	}

	if !filepath.IsAbs(c.FrontendDir) {
		t.Errorf("FrontendDir %q is not absolute", c.FrontendDir)
	}
}

func TestLoad_EnvOverrides(t *testing.T) {
	t.Setenv("DUFFEL_PORT", "9999")
	t.Setenv("DUFFEL_DATA_DIR", "/tmp/testdata")
	t.Setenv("DUFFEL_FRONTEND_DIR", "/tmp/testfrontend")

	c := Load()

	if c.Port != "9999" {
		t.Errorf("Port = %q, want %q", c.Port, "9999")
	}
	if c.DataDir != "/tmp/testdata" {
		t.Errorf("DataDir = %q, want %q", c.DataDir, "/tmp/testdata")
	}
	if c.FrontendDir != "/tmp/testfrontend" {
		t.Errorf("FrontendDir = %q, want %q", c.FrontendDir, "/tmp/testfrontend")
	}
}
