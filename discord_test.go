package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

func TestDiscordWebhookNilSafe(t *testing.T) {
	var d *DiscordWebhook // nil
	// Must not panic.
	d.LogFirst("alice", 1, 5, time.Now())
}

func TestNewDiscordWebhookEmpty(t *testing.T) {
	if d := NewDiscordWebhook(""); d != nil {
		t.Fatalf("NewDiscordWebhook(\"\") = %v, want nil", d)
	}
}

func TestDiscordWebhookLogFirstPayload(t *testing.T) {
	var (
		mu       sync.Mutex
		gotBody  []byte
		gotPath  string
		gotCT    string
		requests int
	)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read body: %v", err)
		}
		mu.Lock()
		gotBody = body
		gotPath = r.URL.Path
		gotCT = r.Header.Get("Content-Type")
		requests++
		mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	d := NewDiscordWebhook(srv.URL + "/api/webhooks/123/abc")
	if d == nil {
		t.Fatal("NewDiscordWebhook returned nil for non-empty url")
	}

	at := time.Date(2026, 5, 27, 12, 34, 56, 0, time.UTC)
	d.LogFirst("Alice", 2, 7, at)

	mu.Lock()
	defer mu.Unlock()

	if requests != 1 {
		t.Fatalf("requests = %d, want 1", requests)
	}
	if gotPath != "/api/webhooks/123/abc" {
		t.Errorf("path = %q, want /api/webhooks/123/abc", gotPath)
	}
	if gotCT != "application/json" {
		t.Errorf("content-type = %q, want application/json", gotCT)
	}

	var p discordPayload
	if err := json.Unmarshal(gotBody, &p); err != nil {
		t.Fatalf("unmarshal body: %v", err)
	}
	if len(p.Embeds) != 1 {
		t.Fatalf("embeds = %d, want 1", len(p.Embeds))
	}
	e := p.Embeds[0]
	if e.Title != "🏆 Alice claimed FIRST" {
		t.Errorf("title = %q", e.Title)
	}
	if e.Timestamp != "2026-05-27T12:34:56Z" {
		t.Errorf("timestamp = %q", e.Timestamp)
	}
	wantFields := map[string]string{"Total": "7", "Rank": "#2"}
	for _, f := range e.Fields {
		want, ok := wantFields[f.Name]
		if !ok {
			t.Errorf("unexpected field %q", f.Name)
			continue
		}
		if f.Value != want {
			t.Errorf("field %s = %q, want %q", f.Name, f.Value, want)
		}
		delete(wantFields, f.Name)
	}
	if len(wantFields) != 0 {
		t.Errorf("missing fields: %v", wantFields)
	}
}
