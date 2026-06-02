package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"
)

// DiscordWebhook posts the FIRST leaderboard to a Discord channel via an
// incoming webhook.
//
// A nil receiver is valid and turns every method into a no-op, so callers can
// always invoke methods without checking whether a webhook is configured.
type DiscordWebhook struct {
	url    string
	client *http.Client
}

// NewDiscordWebhook returns a webhook poster, or nil if url is empty.
func NewDiscordWebhook(url string) *DiscordWebhook {
	if url == "" {
		return nil
	}
	return &DiscordWebhook{
		url:    url,
		client: &http.Client{Timeout: 10 * time.Second},
	}
}

type discordEmbed struct {
	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`
	Color       int    `json:"color,omitempty"`
	Timestamp   string `json:"timestamp,omitempty"`
}

type discordPayload struct {
	Embeds []discordEmbed `json:"embeds"`
}

// LogLeaderboard posts the full FIRST leaderboard as a Discord embed, framed by
// the user who just claimed FIRST. Blocks until the HTTP request completes
// (subject to the client's timeout); callers that must not block should invoke
// it from a goroutine.
func (d *DiscordWebhook) LogLeaderboard(claimedBy string, entries []LeaderboardEntry, at time.Time) {
	if d == nil {
		return
	}

	var sb strings.Builder
	for i, e := range entries {
		if i > 0 {
			sb.WriteByte('\n')
		}
		fmt.Fprintf(&sb, "%s %s (%d)", leaderboardRank(i+1), e.UserName, e.Count)
	}
	description := sb.String()
	if description == "" {
		description = "No one has claimed FIRST yet!"
	}

	embed := discordEmbed{
		Title:       fmt.Sprintf("🏆 %s claimed FIRST", claimedBy),
		Description: description,
		Color:       0xFFD700, // gold
		Timestamp:   at.UTC().Format(time.RFC3339),
	}

	body, err := json.Marshal(discordPayload{Embeds: []discordEmbed{embed}})
	if err != nil {
		log.Printf("discord: marshal payload: %v", err)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "POST", d.url, bytes.NewReader(body))
	if err != nil {
		log.Printf("discord: new request: %v", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := d.client.Do(req)
	if err != nil {
		log.Printf("discord: post: %v", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		log.Printf("discord: %d %s", resp.StatusCode, string(respBody))
	}
}

// leaderboardRank renders a 1-based position as a medal for the top three and a
// plain "N." for the rest.
func leaderboardRank(pos int) string {
	switch pos {
	case 1:
		return "🥇"
	case 2:
		return "🥈"
	case 3:
		return "🥉"
	default:
		return fmt.Sprintf("%d.", pos)
	}
}
