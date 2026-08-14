package internal

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// newTestStore builds a Store over a fresh temporary directory. Nothing global
// is touched, so tests using it can run in parallel.
func newTestStore(t *testing.T) *Store {
	t.Helper()

	store, err := NewStore(filepath.Join(t.TempDir(), "state.yml"))
	if err != nil {
		t.Fatalf("NewStore() error: %v", err)
	}

	return store
}

func TestNewStoreCreatesDirectoryAndFile(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "nested", "dir", "state.yml")

	if _, err := NewStore(path); err != nil {
		t.Fatalf("NewStore() error: %v", err)
	}

	if _, err := os.Stat(path); err != nil {
		t.Errorf("expected state file to exist after NewStore(): %v", err)
	}
}

func TestNewStoreLeavesAnExistingFileAlone(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "state.yml")
	if err := os.WriteFile(path, []byte("wanip: 203.0.113.1\n"), 0600); err != nil {
		t.Fatalf("seeding state file: %v", err)
	}

	store, err := NewStore(path)
	if err != nil {
		t.Fatalf("NewStore() error: %v", err)
	}

	s, err := store.Snapshot()
	if err != nil {
		t.Fatalf("Snapshot() error: %v", err)
	}
	if s.WanIP != "203.0.113.1" {
		t.Errorf("WanIP = %q, want the pre-existing %q — NewStore must not truncate", s.WanIP, "203.0.113.1")
	}
}

func TestNewStoreFailsOnUnusableLocation(t *testing.T) {
	t.Parallel()

	// A regular file cannot also be a directory.
	blocker := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(blocker, []byte("x"), 0600); err != nil {
		t.Fatalf("writing blocker file: %v", err)
	}

	if _, err := NewStore(filepath.Join(blocker, "state.yml")); err == nil {
		t.Error("NewStore() on an unusable path = nil, want an error at startup")
	}
}

func TestSnapshotOfFreshStoreIsUsable(t *testing.T) {
	t.Parallel()

	s, err := newTestStore(t).Snapshot()
	if err != nil {
		t.Fatalf("Snapshot() error: %v", err)
	}

	if s.Routers == nil {
		t.Fatal("expected Routers map to be initialized, got nil")
	}
	// Assignment must not panic (regression for the nil-map bug).
	s.Routers["router"] = Router{Rule: "Host(`x.example.com`)"}
}

func TestStoreRoundTrip(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)

	if err := store.SetWanIP("203.0.113.42"); err != nil {
		t.Fatalf("SetWanIP() error: %v", err)
	}
	if err := store.RecordRouter("my-router", Router{Rule: "Host(`my.example.com`)"}); err != nil {
		t.Fatalf("RecordRouter() error: %v", err)
	}

	s, err := store.Snapshot()
	if err != nil {
		t.Fatalf("Snapshot() error: %v", err)
	}
	if s.WanIP != "203.0.113.42" {
		t.Errorf("WanIP = %q, want %q", s.WanIP, "203.0.113.42")
	}
	if s.Routers["my-router"].Rule != "Host(`my.example.com`)" {
		t.Errorf("Router rule = %q, want %q", s.Routers["my-router"].Rule, "Host(`my.example.com`)")
	}

	// Recording a router must not disturb the WAN IP, and vice versa.
	if err := store.ForgetRouter("my-router"); err != nil {
		t.Fatalf("ForgetRouter() error: %v", err)
	}

	s, err = store.Snapshot()
	if err != nil {
		t.Fatalf("Snapshot() error: %v", err)
	}
	if _, ok := s.Routers["my-router"]; ok {
		t.Error("router should be gone after ForgetRouter()")
	}
	if s.WanIP != "203.0.113.42" {
		t.Errorf("WanIP = %q, want it untouched by ForgetRouter()", s.WanIP)
	}
}

// A snapshot is a copy: mutating it must not reach the store.
func TestSnapshotIsACopy(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	if err := store.RecordRouter("web", Router{Rule: "Host(`web.example.com`)"}); err != nil {
		t.Fatalf("RecordRouter() error: %v", err)
	}

	s, err := store.Snapshot()
	if err != nil {
		t.Fatalf("Snapshot() error: %v", err)
	}
	delete(s.Routers, "web")
	s.WanIP = "198.51.100.1"

	fresh, err := store.Snapshot()
	if err != nil {
		t.Fatalf("Snapshot() error: %v", err)
	}
	if _, ok := fresh.Routers["web"]; !ok {
		t.Error("mutating a snapshot removed the router from the store")
	}
	if fresh.WanIP != "" {
		t.Errorf("mutating a snapshot changed the store's WanIP to %q", fresh.WanIP)
	}
}

// Not knowing what we manage must surface as an error, never as "nothing is
// managed" — that would re-add every record, or delete every record.
func TestUnparseableStateFileIsAnError(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "state.yml")
	if err := os.WriteFile(path, []byte("routers: [this is not: valid yaml"), 0600); err != nil {
		t.Fatalf("seeding state file: %v", err)
	}

	store, err := NewStore(path)
	if err != nil {
		t.Fatalf("NewStore() error: %v", err)
	}

	if _, err := store.Snapshot(); err == nil {
		t.Error("Snapshot() on an unparseable file = nil, want an error")
	}
	if err := store.SetWanIP("203.0.113.1"); err == nil {
		t.Error("SetWanIP() on an unparseable file = nil, want an error rather than a silent overwrite")
	}
}

// Writes go through a temporary file and a rename, so a reader never sees a
// half-written state and no debris is left behind.
func TestWritesLeaveNoTemporaryFiles(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "state.yml"))
	if err != nil {
		t.Fatalf("NewStore() error: %v", err)
	}

	for i := range 5 {
		if err := store.RecordRouter(string(rune('a'+i)), Router{Rule: "Host(`x.example.com`)"}); err != nil {
			t.Fatalf("RecordRouter() error: %v", err)
		}
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("reading state directory: %v", err)
	}

	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".state-") {
			t.Errorf("temporary file %q left behind", entry.Name())
		}
	}
	if len(entries) != 1 {
		t.Errorf("state directory holds %d entries, want just the state file", len(entries))
	}
}

// Concurrent changes must all survive. Each is a read-modify-write, so if the
// cycle were not held under one lock, writers would overwrite each other with
// copies read before the other's change existed.
func TestConcurrentUpdatesAllSurvive(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)

	const routers = 20

	var wg sync.WaitGroup
	errs := make(chan error, routers+1)

	for i := range routers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := store.RecordRouter(string(rune('a'+i)), Router{Rule: "Host(`x.example.com`)"}); err != nil {
				errs <- err
			}
		}()
	}

	// A different field, changing at the same time.
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := store.SetWanIP("203.0.113.7"); err != nil {
			errs <- err
		}
	}()

	wg.Wait()
	close(errs)

	for err := range errs {
		t.Fatalf("concurrent update error: %v", err)
	}

	s, err := store.Snapshot()
	if err != nil {
		t.Fatalf("Snapshot() error: %v", err)
	}
	if len(s.Routers) != routers {
		t.Errorf("state holds %d routers, want all %d — updates were lost", len(s.Routers), routers)
	}
	if s.WanIP != "203.0.113.7" {
		t.Errorf("state WanIP = %q, want %q — the router writes overwrote it", s.WanIP, "203.0.113.7")
	}
}
