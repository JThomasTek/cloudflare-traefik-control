package internal

import (
	"context"
	"testing"
)

// The two reconcile paths run from separate goroutines and both follow a
// read-modify-write over the same state. If the read and the write are guarded
// individually rather than as one cycle, whichever writes second overwrites the
// other's change with the copy it read before that change existed.
//
// This is a lost update, not a data race: every individual access is properly
// synchronised, so the race detector cannot see it. Only the interleaving shows
// it, so the test forces the interleaving rather than hoping to hit it.
//
// The config reconcile is suspended inside Zone.Add — holding a state copy that
// still has the old WAN IP — while the WAN IP reconcile runs to completion.
func TestReconcile_ConcurrentPathsDoNotLoseEachOthersWrites(t *testing.T) {
	t.Parallel()

	const (
		oldIP = "203.0.113.1"
		newIP = "203.0.113.2"
	)

	r, zone := newTestReconciler(t, "", "^$")
	seedState(t, r.store, oldIP, nil)

	addEntered := make(chan struct{})
	releaseAdd := make(chan struct{})

	zone.addHook = func(router string) {
		close(addEntered)
		<-releaseAdd
	}

	cfg := TraefikConfig{}
	cfg.HTTP.Routers = map[string]Router{"web": {Rule: "Host(`web.example.com`)"}}

	configDone := make(chan error, 1)
	go func() {
		configDone <- r.CompareStateToConfig(context.Background(), cfg)
	}()

	// Wait until the config reconcile is mid-flight, then run the WAN IP
	// reconcile all the way through underneath it.
	<-addEntered

	if err := r.CompareStateToWanIP(context.Background(), newIP); err != nil {
		t.Fatalf("CompareStateToWanIP() error: %v", err)
	}

	close(releaseAdd)

	if err := <-configDone; err != nil {
		t.Fatalf("CompareStateToConfig() error: %v", err)
	}

	s := snapshot(t, r.store)

	// Both changes were committed, so both must have survived.
	if _, ok := s.Routers["web"]; !ok {
		t.Error("router 'web' is missing from state: the WAN IP reconcile overwrote the config reconcile's change")
	}
	if s.WanIP != newIP {
		t.Errorf("state WanIP = %q, want %q: the config reconcile overwrote the WAN IP reconcile's change", s.WanIP, newIP)
	}
}
