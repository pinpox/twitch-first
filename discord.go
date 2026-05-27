package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"
)

// DiscordWebhook posts FIRST events to a Discord channel via an incoming webhook.
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

type discordEmbedField struct {
	Name   string `json:"name"`
	Value  string `json:"value"`
	Inline bool   `json:"inline,omitempty"`
}

type discordEmbed struct {
	Title     string              `json:"title,omitempty"`
	Color     int                 `json:"color,omitempty"`
	Fields    []discordEmbedField `json:"fields,omitempty"`
	Timestamp string              `json:"timestamp,omitempty"`
}

type discordPayload struct {
	Embeds []discordEmbed `json:"embeds"`
}

// LogFirst posts an embed describing a FIRST claim. Blocks until the HTTP
// request completes (subject to the client's timeout); callers that must not
// block should invoke it from a goroutine.
func (d *DiscordWebhook) LogFirst(userName string, rank, count int, at time.Time) {
	if d == nil {
		return
	}

	embed := discordEmbed{
		Title:     fmt.Sprintf("🏆 %s claimed FIRST", userName),
		Color:     0xFFD700, // gold
		Timestamp: at.UTC().Format(time.RFC3339),
		Fields: []discordEmbedField{
			{Name: "Total", Value: fmt.Sprintf("%d", count), Inline: true},
			{Name: "Rank", Value: fmt.Sprintf("#%d", rank), Inline: true},
		},
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
