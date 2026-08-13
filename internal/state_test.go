package internal

import (
	"os"
	"path/filepath"
	"testing"
)

// useTempState points the package state path vars at a temporary directory for
// the duration of a test and restores them afterwards.
func useTempState(t *testing.T) {
	t.Helper()

	dir := t.TempDir()
	origFolder, origFile := stateFolder, stateFile
	stateFolder = dir + string(os.PathSeparator)
	stateFile = filepath.Join(dir, "state.yml")
	t.Cleanup(func() {
		stateFolder = origFolder
		stateFile = origFile
	})
}

func TestGetStateInitializesRoutersMap(t *testing.T) {
	t.Run("fresh empty state file", func(t *testing.T) {
		useTempState(t)

		s, err := getState()
		if err != nil {
			t.Fatalf("getState() error: %v", err)
		}
		if s.Routers == nil {
			t.Fatal("expected Routers map to be initialized, got nil")
		}
		// Assignment must not panic (regression for the nil-map bug).
		s.Routers["router"] = Router{Rule: "Host(`x.example.com`)"}
	})

	t.Run("state file without routers key", func(t *testing.T) {
		useTempState(t)
		if err := os.WriteFile(stateFile, []byte("wanip: 203.0.113.1\n"), 0600); err != nil {
			t.Fatalf("seeding state file: %v", err)
		}

		s, err := getState()
		if err != nil {
			t.Fatalf("getState() error: %v", err)
		}
		if s.Routers == nil {
			t.Fatal("expected Routers map to be initialized, got nil")
		}
	})
}

func TestWriteThenGetStateRoundTrip(t *testing.T) {
	useTempState(t)

	want := state{
		WanIP: "203.0.113.42",
		Routers: map[string]Router{
			"my-router": {Rule: "Host(`my.example.com`)"},
		},
	}

	if err := writeState(want); err != nil {
		t.Fatalf("writeState() error: %v", err)
	}

	got, err := getState()
	if err != nil {
		t.Fatalf("getState() error: %v", err)
	}
	if got.WanIP != want.WanIP {
		t.Errorf("WanIP = %q, want %q", got.WanIP, want.WanIP)
	}
	if got.Routers["my-router"].Rule != want.Routers["my-router"].Rule {
		t.Errorf("Router rule = %q, want %q", got.Routers["my-router"].Rule, want.Routers["my-router"].Rule)
	}
}
