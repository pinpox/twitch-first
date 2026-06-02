package main

import (
	"path/filepath"
	"testing"
)

func newTestDB(t *testing.T) *DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.sqlite")
	db, err := NewDB(path)
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func record(t *testing.T, db *DB, id, name string, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		if err := db.RecordFirst(id, name); err != nil {
			t.Fatalf("RecordFirst(%s): %v", id, err)
		}
	}
}

func TestUserRank(t *testing.T) {
	db := newTestDB(t)

	// alice: 5, bob: 3, carol: 3, dave: 1
	record(t, db, "1", "Alice", 5)
	record(t, db, "2", "Bob", 3)
	record(t, db, "3", "Carol", 3)
	record(t, db, "4", "Dave", 1)

	cases := []struct {
		name      string
		query     string
		wantRank  int
		wantCount int
	}{
		{"top", "Alice", 1, 5},
		{"case-insensitive", "alice", 1, 5},
		{"tied second", "Bob", 2, 3},
		{"tied second other", "carol", 2, 3},
		{"after ties", "Dave", 4, 1},
		{"never claimed", "eve", 0, 0},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rank, count, err := db.UserRank(c.query)
			if err != nil {
				t.Fatalf("UserRank: %v", err)
			}
			if rank != c.wantRank || count != c.wantCount {
				t.Errorf("UserRank(%q) = (rank=%d, count=%d), want (rank=%d, count=%d)",
					c.query, rank, count, c.wantRank, c.wantCount)
			}
		})
	}
}

func TestUserRankEmpty(t *testing.T) {
	db := newTestDB(t)
	rank, count, err := db.UserRank("nobody")
	if err != nil {
		t.Fatalf("UserRank: %v", err)
	}
	if rank != 0 || count != 0 {
		t.Errorf("UserRank on empty db = (%d, %d), want (0, 0)", rank, count)
	}
}

func TestLeaderboard(t *testing.T) {
	db := newTestDB(t)

	record(t, db, "1", "Alice", 5)
	record(t, db, "2", "Bob", 3)
	record(t, db, "3", "Carol", 2)
	record(t, db, "4", "Dave", 1)

	// A positive limit caps the result (used by IRC).
	top, err := db.Leaderboard(2)
	if err != nil {
		t.Fatalf("Leaderboard(2): %v", err)
	}
	if len(top) != 2 {
		t.Fatalf("Leaderboard(2) = %d entries, want 2", len(top))
	}
	if top[0].UserName != "Alice" || top[0].Count != 5 {
		t.Errorf("top[0] = %+v, want Alice/5", top[0])
	}
	if top[1].UserName != "Bob" || top[1].Count != 3 {
		t.Errorf("top[1] = %+v, want Bob/3", top[1])
	}

	// FullLeaderboard returns every user, ignoring any cap (used by Discord).
	full, err := db.FullLeaderboard()
	if err != nil {
		t.Fatalf("FullLeaderboard: %v", err)
	}
	wantOrder := []string{"Alice", "Bob", "Carol", "Dave"}
	if len(full) != len(wantOrder) {
		t.Fatalf("FullLeaderboard = %d entries, want %d", len(full), len(wantOrder))
	}
	for i, name := range wantOrder {
		if full[i].UserName != name {
			t.Errorf("full[%d] = %q, want %q", i, full[i].UserName, name)
		}
	}
}

func TestFullLeaderboardEmpty(t *testing.T) {
	db := newTestDB(t)
	entries, err := db.FullLeaderboard()
	if err != nil {
		t.Fatalf("FullLeaderboard: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("FullLeaderboard on empty db = %d entries, want 0", len(entries))
	}
}
