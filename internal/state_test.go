package internal

import (
	"os"
	"path/filepath"
	"regexp"
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

// stubCloudflare replaces the reconcile seam with stubs and restores them after
// the test.
func stubCloudflare(t *testing.T, add func(string, string, string) error, del func(string) error, upd func(state) error) {
	t.Helper()

	origAdd, origDel, origUpd := addSubdomain, deleteSubdomain, updateWanIP
	if add != nil {
		addSubdomain = add
	}
	if del != nil {
		deleteSubdomain = del
	}
	if upd != nil {
		updateWanIP = upd
	}
	t.Cleanup(func() {
		addSubdomain = origAdd
		deleteSubdomain = origDel
		updateWanIP = origUpd
	})
}

func TestCleanRule(t *testing.T) {
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

func TestCompareStateToConfig_AddsNewRouter(t *testing.T) {
	useTempState(t)

	var added []string
	stubCloudflare(t, func(name, host, ip string) error {
		added = append(added, name)
		return nil
	}, nil, nil)

	cfg := TraefikConfig{}
	cfg.HTTP.Routers = map[string]Router{"web": {Rule: "Host(`web.example.com`)"}}

	if err := CompareStateToConfig(cfg, regexp.MustCompile("^$")); err != nil {
		t.Fatalf("CompareStateToConfig() error: %v", err)
	}

	if len(added) != 1 || added[0] != "web" {
		t.Fatalf("expected addSubdomain called for 'web', got %v", added)
	}

	s, _ := getState()
	if _, ok := s.Routers["web"]; !ok {
		t.Error("expected 'web' to be recorded in state after successful add")
	}
}

func TestCompareStateToConfig_IgnoredRouterNotAdded(t *testing.T) {
	useTempState(t)

	var added []string
	stubCloudflare(t, func(name, host, ip string) error {
		added = append(added, name)
		return nil
	}, nil, nil)

	cfg := TraefikConfig{}
	cfg.HTTP.Routers = map[string]Router{"local": {Rule: "Host(`x.local.example.com`)"}}

	ignore := regexp.MustCompile(`^[a-zA-Z0-9-]+\.local\.example\.com$`)
	if err := CompareStateToConfig(cfg, ignore); err != nil {
		t.Fatalf("CompareStateToConfig() error: %v", err)
	}

	if len(added) != 0 {
		t.Fatalf("expected no add for ignored host, got %v", added)
	}
	s, _ := getState()
	if _, ok := s.Routers["local"]; ok {
		t.Error("ignored router should not be recorded in state")
	}
}

func TestCompareStateToConfig_FailedAddNotRecorded(t *testing.T) {
	useTempState(t)

	stubCloudflare(t, func(name, host, ip string) error {
		return errStub
	}, nil, nil)

	cfg := TraefikConfig{}
	cfg.HTTP.Routers = map[string]Router{"web": {Rule: "Host(`web.example.com`)"}}

	if err := CompareStateToConfig(cfg, regexp.MustCompile("^$")); err != nil {
		t.Fatalf("CompareStateToConfig() error: %v", err)
	}

	s, _ := getState()
	if _, ok := s.Routers["web"]; ok {
		t.Error("router must NOT be recorded in state when Cloudflare add fails")
	}
}

func TestCompareStateToConfig_RemovesRouter(t *testing.T) {
	useTempState(t)

	// Seed state with a router that is no longer present in config.
	if err := writeState(state{
		WanIP:   "203.0.113.1",
		Routers: map[string]Router{"old": {Rule: "Host(`old.example.com`)"}},
	}); err != nil {
		t.Fatalf("seeding state: %v", err)
	}

	var deleted []string
	stubCloudflare(t, nil, func(name string) error {
		deleted = append(deleted, name)
		return nil
	}, nil)

	cfg := TraefikConfig{}
	cfg.HTTP.Routers = map[string]Router{}

	if err := CompareStateToConfig(cfg, regexp.MustCompile("^$")); err != nil {
		t.Fatalf("CompareStateToConfig() error: %v", err)
	}

	if len(deleted) != 1 || deleted[0] != "old" {
		t.Fatalf("expected deleteSubdomain called for 'old', got %v", deleted)
	}
	s, _ := getState()
	if _, ok := s.Routers["old"]; ok {
		t.Error("expected 'old' to be removed from state after successful delete")
	}
}

func TestCompareStateToConfig_FailedDeleteKeepsState(t *testing.T) {
	useTempState(t)

	if err := writeState(state{
		Routers: map[string]Router{"old": {Rule: "Host(`old.example.com`)"}},
	}); err != nil {
		t.Fatalf("seeding state: %v", err)
	}

	stubCloudflare(t, nil, func(name string) error {
		return errStub
	}, nil)

	cfg := TraefikConfig{}
	cfg.HTTP.Routers = map[string]Router{}

	if err := CompareStateToConfig(cfg, regexp.MustCompile("^$")); err != nil {
		t.Fatalf("CompareStateToConfig() error: %v", err)
	}

	s, _ := getState()
	if _, ok := s.Routers["old"]; !ok {
		t.Error("router must remain in state when Cloudflare delete fails")
	}
}

func TestCompareStateToWanIP_UpdatesOnChange(t *testing.T) {
	useTempState(t)

	if err := writeState(state{WanIP: "203.0.113.1", Routers: map[string]Router{}}); err != nil {
		t.Fatalf("seeding state: %v", err)
	}

	var updatedTo string
	stubCloudflare(t, nil, nil, func(s state) error {
		updatedTo = s.WanIP
		return nil
	})

	if err := CompareStateToWanIP("203.0.113.2"); err != nil {
		t.Fatalf("CompareStateToWanIP() error: %v", err)
	}

	if updatedTo != "203.0.113.2" {
		t.Errorf("updateWanIP called with %q, want %q", updatedTo, "203.0.113.2")
	}
	s, _ := getState()
	if s.WanIP != "203.0.113.2" {
		t.Errorf("state WanIP = %q, want %q", s.WanIP, "203.0.113.2")
	}
}

func TestCompareStateToWanIP_NoChangeSkipsUpdate(t *testing.T) {
	useTempState(t)

	if err := writeState(state{WanIP: "203.0.113.1", Routers: map[string]Router{}}); err != nil {
		t.Fatalf("seeding state: %v", err)
	}

	called := false
	stubCloudflare(t, nil, nil, func(s state) error {
		called = true
		return nil
	})

	if err := CompareStateToWanIP("203.0.113.1"); err != nil {
		t.Fatalf("CompareStateToWanIP() error: %v", err)
	}
	if called {
		t.Error("updateWanIP should not be called when the WAN IP is unchanged")
	}
}
