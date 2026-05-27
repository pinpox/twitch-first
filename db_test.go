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
