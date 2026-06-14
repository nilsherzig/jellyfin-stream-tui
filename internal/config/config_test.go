package config

import (
	"os"
	"path/filepath"
	"testing"
)

// writeTemp writes content to a temp file and returns the path.
func writeTemp(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatalf("write temp: %v", err)
	}
	return p
}

// Positive: a complete config loads correctly.
func TestLoad_Valid(t *testing.T) {
	p := writeTemp(t, "server: https://jf.example.com\nusername: nils\npassword: test\n")

	c, err := Load(p)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.Server != "https://jf.example.com" || c.Username != "nils" || c.Password != "test" {
		t.Fatalf("loaded wrong values: %+v", c)
	}
}

// Negative: a missing file must return an error.
func TestLoad_MissingFile(t *testing.T) {
	if _, err := Load(filepath.Join(t.TempDir(), "nope.yaml")); err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
}

// Negative: broken YAML must return an error.
func TestLoad_InvalidYAML(t *testing.T) {
	p := writeTemp(t, "server: [unclosed")
	if _, err := Load(p); err == nil {
		t.Fatal("expected error for broken YAML, got nil")
	}
}

// Negative: a missing required field (server) must return an error.
func TestLoad_MissingServer(t *testing.T) {
	p := writeTemp(t, "username: nils\npassword: test\n")
	if _, err := Load(p); err == nil {
		t.Fatal("expected error for missing server, got nil")
	}
}
