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

func (d *DB) Leaderboard(limit int) ([]LeaderboardEntry, error) {
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

func (d *DB) Close() error {
	return d.db.Close()
}
