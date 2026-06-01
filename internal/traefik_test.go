package internal

import (
	"os"
	"path/filepath"
	"testing"
)

func writeTempConfig(t *testing.T, contents string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "config.yml")
	if err := os.WriteFile(path, []byte(contents), 0600); err != nil {
		t.Fatalf("writing temp config: %v", err)
	}
	return path
}

func TestReadTraefikConfig_Valid(t *testing.T) {
	// Built with a double-quoted string because the Traefik rule contains
	// backticks, which would terminate a Go raw-string literal.
	wantRule := "Host(`web.example.com`)"
	path := writeTempConfig(t, "http:\n  routers:\n    web:\n      rule: \""+wantRule+"\"\n")

	cfg, err := readTraefikConfig(path)
	if err != nil {
		t.Fatalf("readTraefikConfig() error: %v", err)
	}

	router, ok := cfg.HTTP.Routers["web"]
	if !ok {
		t.Fatal("expected router 'web' to be parsed")
	}
	if router.Rule != wantRule {
		t.Errorf("router rule = %q, want %q", router.Rule, wantRule)
	}
}

func TestReadTraefikConfig_FileNotFound(t *testing.T) {
	_, err := readTraefikConfig(filepath.Join(t.TempDir(), "does-not-exist.yml"))
	if err == nil {
		t.Fatal("expected error for missing config file, got nil")
	}
}

func TestReadTraefikConfig_MalformedYAML(t *testing.T) {
	path := writeTempConfig(t, "http: [this is not: valid yaml")

	_, err := readTraefikConfig(path)
	if err == nil {
		t.Fatal("expected error for malformed YAML, got nil")
	}
}
