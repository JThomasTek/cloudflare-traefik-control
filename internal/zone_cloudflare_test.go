package internal

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cloudflare/cloudflare-go"
)

const testZoneID = "zone123"

// stubRecord is the subset of a Cloudflare DNS record these tests serve.
type stubRecord struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Content string `json:"content"`
	Comment string `json:"comment"`
}

// newTestZone points a real Cloudflare client at a local handler, so the
// adapter is exercised through the same client it uses in production.
func newTestZone(t *testing.T, handler http.HandlerFunc) *cloudflareZone {
	t.Helper()

	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	api, err := cloudflare.NewWithAPIToken("test-token")
	if err != nil {
		t.Fatalf("building Cloudflare client: %v", err)
	}
	api.BaseURL = srv.URL

	return &cloudflareZone{api: api, zoneID: testZoneID}
}

func writeJSON(t *testing.T, w http.ResponseWriter, result any, resultInfo map[string]int) {
	t.Helper()

	body := map[string]any{
		"success":  true,
		"errors":   []any{},
		"messages": []any{},
		"result":   result,
	}
	if resultInfo != nil {
		body["result_info"] = resultInfo
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(body); err != nil {
		t.Errorf("encoding response: %v", err)
	}
}

// writeRecordList serves a complete, single-page listing. The result_info
// terminates the client's automatic pagination.
func writeRecordList(t *testing.T, w http.ResponseWriter, records []stubRecord) {
	t.Helper()

	writeJSON(t, w, records, map[string]int{
		"page":        1,
		"per_page":    100,
		"count":       len(records),
		"total_count": len(records),
		"total_pages": 1,
	})
}

func TestCloudflareZone_AddClaimsRecordWithOwnershipComment(t *testing.T) {
	var body map[string]any
	var path string

	zone := newTestZone(t, func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decoding request body: %v", err)
		}
		writeJSON(t, w, stubRecord{ID: "rec1"}, nil)
	})

	if err := zone.Add(context.Background(), "web", "web.example.com", "203.0.113.1"); err != nil {
		t.Fatalf("Add() error: %v", err)
	}

	if want := "/zones/" + testZoneID + "/dns_records"; path != want {
		t.Errorf("request path = %q, want %q", path, want)
	}

	want := map[string]any{
		"type":    "A",
		"name":    "web.example.com",
		"content": "203.0.113.1",
		"comment": "Managed by ctc: web",
		"proxied": true,
		"ttl":     float64(1), // JSON numbers decode as float64.
	}
	for k, v := range want {
		if body[k] != v {
			t.Errorf("request body[%q] = %v, want %v", k, body[k], v)
		}
	}
}

func TestCloudflareZone_SetIPOnlyTouchesOwnedRecords(t *testing.T) {
	updated := map[string]string{}
	listCalls := 0

	zone := newTestZone(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			listCalls++
			if listCalls > 3 {
				// Guard against a pagination loop hanging the test.
				http.Error(w, "unexpected repeated listing", http.StatusBadRequest)
				return
			}
			writeRecordList(t, w, []stubRecord{
				{ID: "rec1", Name: "web.example.com", Content: "203.0.113.1", Comment: "Managed by ctc: web"},
				{ID: "rec2", Name: "api.example.com", Content: "203.0.113.1", Comment: "Managed by ctc: api"},
				{ID: "rec3", Name: "mail.example.com", Content: "198.51.100.9", Comment: "hand-made, do not touch"},
			})
			return
		}

		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decoding request body: %v", err)
		}

		id := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
		updated[id], _ = body["content"].(string)

		writeJSON(t, w, stubRecord{ID: id}, nil)
	})

	if err := zone.SetIP(context.Background(), "203.0.113.99"); err != nil {
		t.Fatalf("SetIP() error: %v", err)
	}

	if len(updated) != 2 {
		t.Fatalf("updated %d records (%v), want exactly the 2 owned ones", len(updated), updated)
	}
	for _, id := range []string{"rec1", "rec2"} {
		if updated[id] != "203.0.113.99" {
			t.Errorf("record %s updated to %q, want %q", id, updated[id], "203.0.113.99")
		}
	}
	if _, ok := updated["rec3"]; ok {
		t.Error("record without our ownership comment must not be updated")
	}
}

func TestCloudflareZone_SetIPAggregatesFailures(t *testing.T) {
	updated := map[string]bool{}

	zone := newTestZone(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			writeRecordList(t, w, []stubRecord{
				{ID: "rec1", Comment: "Managed by ctc: web"},
				{ID: "rec2", Comment: "Managed by ctc: api"},
			})
			return
		}

		id := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
		if id == "rec1" {
			// 400 is not retried by the client, unlike 429/5xx.
			http.Error(w, `{"success":false,"errors":[{"code":1000,"message":"nope"}]}`, http.StatusBadRequest)
			return
		}

		updated[id] = true
		writeJSON(t, w, stubRecord{ID: id}, nil)
	})

	err := zone.SetIP(context.Background(), "203.0.113.99")
	if err == nil {
		t.Fatal("SetIP() expected an error when a record update fails")
	}

	// The failing record must not stop the others from moving.
	if !updated["rec2"] {
		t.Error("rec2 should still have been updated after rec1 failed")
	}
	if !strings.Contains(err.Error(), "web") {
		t.Errorf("error %q should name the router whose record failed", err)
	}
}

func TestCloudflareZone_RemoveDeletesOwnedRecord(t *testing.T) {
	deleted := ""

	zone := newTestZone(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			writeRecordList(t, w, []stubRecord{
				{ID: "rec1", Comment: "Managed by ctc: web"},
				{ID: "rec2", Comment: "Managed by ctc: api"},
			})
			return
		}

		if r.Method == http.MethodDelete {
			deleted = r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
			writeJSON(t, w, stubRecord{ID: deleted}, nil)
			return
		}

		t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
	})

	if err := zone.Remove(context.Background(), "api"); err != nil {
		t.Fatalf("Remove() error: %v", err)
	}

	if deleted != "rec2" {
		t.Errorf("deleted record %q, want rec2 (the one owned by router 'api')", deleted)
	}
}

// Remove is idempotent: the reconciler retries deletes after a crash, so a
// router that owns no record must not produce an error.
func TestCloudflareZone_RemoveUnownedRouterIsNoOp(t *testing.T) {
	zone := newTestZone(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			writeRecordList(t, w, []stubRecord{
				{ID: "rec3", Comment: "hand-made, do not touch"},
			})
			return
		}

		t.Errorf("unexpected %s %s: nothing is owned, so nothing should be deleted", r.Method, r.URL.Path)
	})

	if err := zone.Remove(context.Background(), "web"); err != nil {
		t.Errorf("Remove() on an unowned router = %v, want nil", err)
	}
}
