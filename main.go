package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"slices"
	"sync"
)

func main() {
	clientID := requiredEnv("TWITCH_CLIENT_ID")
	clientSecret := requiredEnv("TWITCH_CLIENT_SECRET")
	channel := requiredEnv("TWITCH_CHANNEL")
	rewardID := os.Getenv("TWITCH_REWARD_ID") // optional: filter to specific reward

	dbPath := os.Getenv("TWITCH_FIRST_DB")
	if dbPath == "" {
		dbPath = "firsts.db"
	}

	tokenPath := os.Getenv("TWITCH_TOKEN_FILE")
	if tokenPath == "" {
		tokenPath = "token.json"
	}

	// Auth
	accessToken, err := LoadOrAuthorize(clientID, clientSecret, tokenPath)
	if err != nil {
		log.Fatalf("auth: %v", err)
	}
	log.Println("Authenticated with Twitch")

	// Resolve broadcaster ID
	broadcasterID, err := GetBroadcasterID(clientID, accessToken, channel)
	if err != nil {
		log.Fatalf("get broadcaster id: %v", err)
	}
	log.Printf("Broadcaster: %s (ID: %s)", channel, broadcasterID)

	if slices.Contains(os.Args[1:], "--list-rewards") {
		if err := ListRewards(clientID, accessToken, broadcasterID); err != nil {
			log.Fatalf("list rewards: %v", err)
		}
		return
	}

	// Database
	db, err := NewDB(dbPath)
	if err != nil {
		log.Fatalf("db: %v", err)
	}
	defer db.Close()

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	// IRC bot (for chat commands)
	botName := os.Getenv("TWITCH_BOT_NAME")
	if botName == "" {
		botName = channel
	}
	irc := NewIRCBot(db, botName, accessToken, channel)

	// EventSub (for channel point redemptions)
	eventsub := NewEventSubClient(db, irc, clientID, accessToken, broadcasterID, rewardID)

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		if err := irc.Run(ctx); err != nil && ctx.Err() == nil {
			log.Printf("irc error: %v", err)
		}
	}()

	go func() {
		defer wg.Done()
		if err := eventsub.Run(ctx); err != nil && ctx.Err() == nil {
			log.Printf("eventsub error: %v", err)
		}
	}()

	log.Println("twitch-first running. Press Ctrl+C to stop.")
	wg.Wait()
}
