package internal

import (
	"context"
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

// An unreadable config parses to zero routers. Reconciling against that would
// read as "every router was removed" and wipe every record CTC manages, so a
// failed read must abandon the reconcile entirely.
func TestHandleConfigChange_UnreadableConfigDoesNotDeleteRecords(t *testing.T) {
	seeded := map[string]Router{
		"web": {Rule: "Host(`web.example.com`)"},
		"api": {Rule: "Host(`api.example.com`)"},
	}

	t.Run("missing file", func(t *testing.T) {
		t.Parallel()

		r, zone := newTestReconciler(t, filepath.Join(t.TempDir(), "gone.yml"), "^$")
		seedState(t, r.store, "203.0.113.1", seeded)

		r.handleConfigChange(context.Background())

		if len(zone.removes) != 0 {
			t.Errorf("Remove called for %v; an unreadable config must not delete records", zone.removes)
		}
		if got := len(snapshot(t, r.store).Routers); got != len(seeded) {
			t.Errorf("state has %d routers, want %d unchanged", got, len(seeded))
		}
	})

	t.Run("malformed yaml", func(t *testing.T) {
		t.Parallel()

		r, zone := newTestReconciler(t, writeTempConfig(t, "http: [this is not: valid yaml"), "^$")
		seedState(t, r.store, "203.0.113.1", seeded)

		r.handleConfigChange(context.Background())

		if len(zone.removes) != 0 {
			t.Errorf("Remove called for %v; a malformed config must not delete records", zone.removes)
		}
		if got := len(snapshot(t, r.store).Routers); got != len(seeded) {
			t.Errorf("state has %d routers, want %d unchanged", got, len(seeded))
		}
	})
}
