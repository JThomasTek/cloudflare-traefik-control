package internal

import (
	"context"
	"regexp"
	"testing"
)

// newTestReconciler builds a Reconciler over a fake zone and a store in a fresh
// temporary directory. The config file path is only needed by the tests that
// read one from disk.
func newTestReconciler(t *testing.T, configFile string, ignore string) (*Reconciler, *fakeZone) {
	t.Helper()

	zone := newFakeZone()

	return NewReconciler(zone, newTestStore(t), configFile, regexp.MustCompile(ignore)), zone
}

// seedState puts the store into a known starting position.
func seedState(t *testing.T, store *Store, wanIP string, routers map[string]Router) {
	t.Helper()

	if wanIP != "" {
		if err := store.SetWanIP(wanIP); err != nil {
			t.Fatalf("seeding WAN IP: %v", err)
		}
	}

	for name, router := range routers {
		if err := store.RecordRouter(name, router); err != nil {
			t.Fatalf("seeding router %q: %v", name, err)
		}
	}
}

func snapshot(t *testing.T, store *Store) state {
	t.Helper()

	s, err := store.Snapshot()
	if err != nil {
		t.Fatalf("Snapshot() error: %v", err)
	}

	return s
}

func TestCleanRule(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		rule    string
		want    string
		wantErr bool
	}{
		{name: "simple host", rule: "Host(`foo.example.com`)", want: "foo.example.com"},
		{name: "host with additional clauses", rule: "Host(`a.b.com`) && PathPrefix(`/x`)", want: "a.b.com"},
		{name: "missing host clause", rule: "PathPrefix(`/x`)", wantErr: true},
		{name: "unterminated host clause", rule: "Host(`broken", wantErr: true},
		{name: "empty rule", rule: "", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := cleanRule(tt.rule)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("cleanRule(%q) expected error, got nil (result %q)", tt.rule, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("cleanRule(%q) unexpected error: %v", tt.rule, err)
			}
			if got != tt.want {
				t.Errorf("cleanRule(%q) = %q, want %q", tt.rule, got, tt.want)
			}
		})
	}
}

func TestCompareStateToConfig_AddsNewRouter(t *testing.T) {
	t.Parallel()

	r, zone := newTestReconciler(t, "", "^$")
	seedState(t, r.store, "203.0.113.1", nil)

	cfg := TraefikConfig{}
	cfg.HTTP.Routers = map[string]Router{"web": {Rule: "Host(`web.example.com`)"}}

	if err := r.CompareStateToConfig(context.Background(), cfg); err != nil {
		t.Fatalf("CompareStateToConfig() error: %v", err)
	}

	if len(zone.adds) != 1 || zone.adds[0] != "web" {
		t.Fatalf("expected Add called for 'web', got %v", zone.adds)
	}

	// The host, not the raw rule, is what reaches the zone — and it must carry
	// the WAN IP already recorded in state.
	if got := zone.records["web"]; got.host != "web.example.com" || got.ip != "203.0.113.1" {
		t.Errorf("record for 'web' = %+v, want host web.example.com and ip 203.0.113.1", got)
	}

	if _, ok := snapshot(t, r.store).Routers["web"]; !ok {
		t.Error("expected 'web' to be recorded in state after successful add")
	}
}

func TestCompareStateToConfig_IgnoredRouterNotAdded(t *testing.T) {
	t.Parallel()

	r, zone := newTestReconciler(t, "", `^[a-zA-Z0-9-]+\.local\.example\.com$`)

	// A WAN IP is required for the add path to run at all; without one the
	// deferral guard would make this assertion pass for the wrong reason.
	seedState(t, r.store, "203.0.113.1", nil)

	cfg := TraefikConfig{}
	cfg.HTTP.Routers = map[string]Router{"local": {Rule: "Host(`x.local.example.com`)"}}

	if err := r.CompareStateToConfig(context.Background(), cfg); err != nil {
		t.Fatalf("CompareStateToConfig() error: %v", err)
	}

	if len(zone.adds) != 0 {
		t.Fatalf("expected no Add for ignored host, got %v", zone.adds)
	}
	if _, ok := snapshot(t, r.store).Routers["local"]; ok {
		t.Error("ignored router should not be recorded in state")
	}
}

func TestCompareStateToConfig_FailedAddNotRecorded(t *testing.T) {
	t.Parallel()

	r, zone := newTestReconciler(t, "", "^$")
	zone.addErr = errStub

	// As above: without a WAN IP the add would be deferred rather than failed.
	seedState(t, r.store, "203.0.113.1", nil)

	cfg := TraefikConfig{}
	cfg.HTTP.Routers = map[string]Router{"web": {Rule: "Host(`web.example.com`)"}}

	if err := r.CompareStateToConfig(context.Background(), cfg); err != nil {
		t.Fatalf("CompareStateToConfig() error: %v", err)
	}

	if _, ok := snapshot(t, r.store).Routers["web"]; ok {
		t.Error("router must NOT be recorded in state when the zone add fails")
	}
}

func TestCompareStateToConfig_RemovesRouter(t *testing.T) {
	t.Parallel()

	r, zone := newTestReconciler(t, "", "^$")

	// Seed state with a router that is no longer present in config.
	seedState(t, r.store, "203.0.113.1", map[string]Router{"old": {Rule: "Host(`old.example.com`)"}})

	cfg := TraefikConfig{}
	cfg.HTTP.Routers = map[string]Router{}

	if err := r.CompareStateToConfig(context.Background(), cfg); err != nil {
		t.Fatalf("CompareStateToConfig() error: %v", err)
	}

	if len(zone.removes) != 1 || zone.removes[0] != "old" {
		t.Fatalf("expected Remove called for 'old', got %v", zone.removes)
	}
	if _, ok := snapshot(t, r.store).Routers["old"]; ok {
		t.Error("expected 'old' to be removed from state after successful delete")
	}
}

func TestCompareStateToConfig_FailedDeleteKeepsState(t *testing.T) {
	t.Parallel()

	r, zone := newTestReconciler(t, "", "^$")
	zone.removeErr = errStub

	seedState(t, r.store, "", map[string]Router{"old": {Rule: "Host(`old.example.com`)"}})

	cfg := TraefikConfig{}
	cfg.HTTP.Routers = map[string]Router{}

	if err := r.CompareStateToConfig(context.Background(), cfg); err != nil {
		t.Fatalf("CompareStateToConfig() error: %v", err)
	}

	if _, ok := snapshot(t, r.store).Routers["old"]; !ok {
		t.Error("router must remain in state when the zone delete fails")
	}
}

// Both halves matter. Skipping the adds is the point, but the removals have to
// keep running: a guard placed over the whole reconcile would pass the first
// assertion and silently strand records whose routers are gone.
func TestCompareStateToConfig_NoWanIPDefersAddsButStillRemoves(t *testing.T) {
	t.Parallel()

	r, zone := newTestReconciler(t, "", "^$")

	// No WAN IP: this is a fresh state file, before the first WAN IP check.
	seedState(t, r.store, "", map[string]Router{"old": {Rule: "Host(`old.example.com`)"}})

	cfg := TraefikConfig{}
	cfg.HTTP.Routers = map[string]Router{"new": {Rule: "Host(`new.example.com`)"}}

	if err := r.CompareStateToConfig(context.Background(), cfg); err != nil {
		t.Fatalf("CompareStateToConfig() error: %v", err)
	}

	if len(zone.adds) != 0 {
		t.Errorf("no record may be added before the WAN IP is known, got %v", zone.adds)
	}
	if _, ok := snapshot(t, r.store).Routers["new"]; ok {
		t.Error("a deferred router must not be recorded in state, or it is never revisited")
	}

	if len(zone.removes) != 1 || zone.removes[0] != "old" {
		t.Fatalf("removals must run without a WAN IP, got %v", zone.removes)
	}
	if _, ok := snapshot(t, r.store).Routers["old"]; ok {
		t.Error("expected 'old' to be removed from state after successful delete")
	}
}

func TestCompareStateToWanIP_UpdatesOnChange(t *testing.T) {
	t.Parallel()

	r, zone := newTestReconciler(t, "", "^$")
	seedState(t, r.store, "203.0.113.1", nil)

	if err := r.CompareStateToWanIP(context.Background(), "203.0.113.2"); err != nil {
		t.Fatalf("CompareStateToWanIP() error: %v", err)
	}

	if len(zone.setIPs) != 1 || zone.setIPs[0] != "203.0.113.2" {
		t.Errorf("SetIP calls = %v, want one call with 203.0.113.2", zone.setIPs)
	}
	if got := snapshot(t, r.store).WanIP; got != "203.0.113.2" {
		t.Errorf("state WanIP = %q, want %q", got, "203.0.113.2")
	}
}

func TestCompareStateToWanIP_FailedUpdateNotPersisted(t *testing.T) {
	t.Parallel()

	r, zone := newTestReconciler(t, "", "^$")
	zone.setIPErr = errStub

	seedState(t, r.store, "203.0.113.1", nil)

	if err := r.CompareStateToWanIP(context.Background(), "203.0.113.2"); err == nil {
		t.Fatal("expected an error to propagate when SetIP fails")
	}

	// The new IP must NOT be persisted, so the next loop retries the update.
	if got := snapshot(t, r.store).WanIP; got != "203.0.113.1" {
		t.Errorf("state WanIP = %q, want unchanged %q after failed update", got, "203.0.113.1")
	}
}

func TestCompareStateToWanIP_NoChangeSkipsUpdate(t *testing.T) {
	t.Parallel()

	r, zone := newTestReconciler(t, "", "^$")
	seedState(t, r.store, "203.0.113.1", nil)

	if err := r.CompareStateToWanIP(context.Background(), "203.0.113.1"); err != nil {
		t.Fatalf("CompareStateToWanIP() error: %v", err)
	}

	if len(zone.setIPs) != 0 {
		t.Errorf("SetIP should not be called when the WAN IP is unchanged, got %v", zone.setIPs)
	}
}
