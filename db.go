package main

import (
	"database/sql"
	"fmt"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

type DB struct {
	db *sql.DB
}

type LeaderboardEntry struct {
	UserName string
	Count    int
}

func NewDB(path string) (*DB, error) {
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}

	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS firsts (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id TEXT NOT NULL,
			user_name TEXT NOT NULL,
			redeemed_at DATETIME NOT NULL
		)
	`)
	if err != nil {
		return nil, fmt.Errorf("create table: %w", err)
	}

	return &DB{db: db}, nil
}

func (d *DB) RecordFirst(userID, userName string) error {
	_, err := d.db.Exec(
		"INSERT INTO firsts (user_id, user_name, redeemed_at) VALUES (?, ?, ?)",
		userID, userName, time.Now().UTC(),
	)
	return err
}

// Leaderboard returns up to `limit` users ranked by FIRST count, highest first.
// A non-positive limit returns every entry (SQLite treats a negative LIMIT as
// unbounded).
func (d *DB) Leaderboard(limit int) ([]LeaderboardEntry, error) {
	if limit <= 0 {
		limit = -1
	}
	rows, err := d.db.Query(`
		SELECT user_name, COUNT(*) as cnt
		FROM firsts
		GROUP BY user_id
		ORDER BY cnt DESC
		LIMIT ?
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []LeaderboardEntry
	for rows.Next() {
		var e LeaderboardEntry
		if err := rows.Scan(&e.UserName, &e.Count); err != nil {
			return nil, err
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}

// FullLeaderboard returns every user ranked by FIRST count, highest first.
func (d *DB) FullLeaderboard() ([]LeaderboardEntry, error) {
	return d.Leaderboard(-1)
}

// UserRank returns the user's competition rank and total FIRST count.
// Rank is 1-based; tied users share the same rank. A returned count of 0
// means the user has never claimed FIRST (rank is then 0).
// The lookup is case-insensitive on the stored user_name.
func (d *DB) UserRank(userName string) (rank, count int, err error) {
	err = d.db.QueryRow(
		`SELECT COUNT(*) FROM firsts WHERE LOWER(user_name) = LOWER(?)`,
		userName,
	).Scan(&count)
	if err != nil || count == 0 {
		return 0, count, err
	}

	err = d.db.QueryRow(`
		SELECT COUNT(*) FROM (
			SELECT user_id, COUNT(*) AS c
			FROM firsts
			GROUP BY user_id
			HAVING c > ?
		)
	`, count).Scan(&rank)
	if err != nil {
		return 0, 0, err
	}
	return rank + 1, count, nil
}

func (d *DB) Close() error {
	return d.db.Close()
}
