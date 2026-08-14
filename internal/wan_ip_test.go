package internal

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// useWANIPServer points the WAN IP lookup at the given handler for the test.
func useWANIPServer(t *testing.T, srv *httptest.Server) {
	t.Helper()

	origURL, origClient := wanIPURL, wanIPClient
	wanIPURL = srv.URL
	wanIPClient = srv.Client()
	t.Cleanup(func() {
		wanIPURL = origURL
		wanIPClient = origClient
	})
}

func TestGetWANIP_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// icanhazip returns the IP with a trailing newline.
		w.Write([]byte("203.0.113.7\n"))
	}))
	defer srv.Close()
	useWANIPServer(t, srv)

	got, err := GetWANIP()
	if err != nil {
		t.Fatalf("GetWANIP() unexpected error: %v", err)
	}
	if got != "203.0.113.7" {
		t.Errorf("GetWANIP() = %q, want %q (whitespace must be trimmed)", got, "203.0.113.7")
	}
}

func TestGetWANIP_Non200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()
	useWANIPServer(t, srv)

	// A non-200 response must not be trimmed and returned as if it were an IP.
	got, err := GetWANIP()
	if err == nil {
		t.Fatalf("GetWANIP() expected an error on HTTP 500, got %q", got)
	}
}

func TestGetWANIP_HTTPError(t *testing.T) {
	// Start a server then close it so the request fails at the transport level.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	useWANIPServer(t, srv)
	srv.Close()

	// Must return an error rather than panicking on a nil response body.
	got, err := GetWANIP()
	if err == nil {
		t.Fatalf("GetWANIP() expected an error after server shutdown, got %q", got)
	}
}

func TestWatchWanIP_StopsOnContextCancel(t *testing.T) {
	// The interval is deliberately far longer than the test is willing to wait:
	// cancellation must be noticed while the loop is between ticks, not only
	// once a tick has fired.
	r, _ := newTestReconciler(t, "", "^$")

	ctx, cancel := context.WithCancel(context.Background())

	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		r.WatchWanIP(ctx, time.Hour)
	}()

	cancel()

	select {
	case <-stopped:
	case <-time.After(5 * time.Second):
		t.Fatal("WatchWanIP() did not return after its context was cancelled")
	}
}
