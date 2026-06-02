package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestDiscordWebhookNilSafe(t *testing.T) {
	var d *DiscordWebhook // nil
	// Must not panic.
	d.LogLeaderboard("alice", nil, time.Now())
}

func TestNewDiscordWebhookEmpty(t *testing.T) {
	if d := NewDiscordWebhook(""); d != nil {
		t.Fatalf("NewDiscordWebhook(\"\") = %v, want nil", d)
	}
}

func TestDiscordWebhookLogLeaderboardPayload(t *testing.T) {
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
	entries := []LeaderboardEntry{
		{UserName: "Alice", Count: 12},
		{UserName: "Bob", Count: 11},
		{UserName: "Carol", Count: 10},
		{UserName: "Dave", Count: 9},
		{UserName: "Eve", Count: 8},
		{UserName: "Frank", Count: 7},
		{UserName: "Grace", Count: 6},
		{UserName: "Heidi", Count: 5},
		{UserName: "Ivan", Count: 4},
		{UserName: "Judy", Count: 3},
		{UserName: "Mallory", Count: 2},
		{UserName: "Niaj", Count: 1},
	}
	d.LogLeaderboard("Alice", entries, at)

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
	expectedRanks := []string{"🥇", "🥈", "🥉", "4.", "5.", "6.", "7.", "8.", "9.", "10.", "11.", "12."}
	lines := strings.Split(e.Description, "\n")
	if len(lines) != len(entries) {
		t.Fatalf("description has %d lines, want %d:\n%s", len(lines), len(entries), e.Description)
	}
	for i, ent := range entries {
		line := lines[i]
		if !strings.HasPrefix(line, expectedRanks[i]+" ") {
			t.Errorf("line %d = %q, want rank prefix %q", i, line, expectedRanks[i])
		}
		if !strings.Contains(line, ent.UserName) {
			t.Errorf("line %d = %q, missing user %q", i, line, ent.UserName)
		}
		if want := "(" + strconv.Itoa(ent.Count) + ")"; !strings.Contains(line, want) {
			t.Errorf("line %d = %q, missing count %q", i, line, want)
		}
	}
}
