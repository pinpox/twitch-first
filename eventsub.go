package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/gorilla/websocket"
)

const (
	twitchEventSubWSURL = "wss://eventsub.wss.twitch.tv/ws"
)

type EventSubClient struct {
	db            *DB
	irc           *IRCBot
	tm            *TokenManager
	discord       *DiscordWebhook
	clientID      string
	accessToken   string
	broadcasterID string
	rewardID      string
	sessionID     string
}

// EventSub WebSocket message types
type wsMessage struct {
	Metadata struct {
		MessageID   string `json:"message_id"`
		MessageType string `json:"message_type"`
		Timestamp   string `json:"message_timestamp"`
	} `json:"metadata"`
	Payload json.RawMessage `json:"payload"`
}

type welcomePayload struct {
	Session struct {
		ID                      string `json:"id"`
		KeepaliveTimeoutSeconds int    `json:"keepalive_timeout_seconds"`
	} `json:"session"`
}

type notificationPayload struct {
	Subscription struct {
		Type string `json:"type"`
	} `json:"subscription"`
	Event json.RawMessage `json:"event"`
}

type redemptionEvent struct {
	UserID    string `json:"user_id"`
	UserLogin string `json:"user_login"`
	UserName  string `json:"user_name"`
	Reward    struct {
		ID    string `json:"id"`
		Title string `json:"title"`
	} `json:"reward"`
}

type raidEvent struct {
	FromBroadcasterUserLogin string `json:"from_broadcaster_user_login"`
	FromBroadcasterUserName  string `json:"from_broadcaster_user_name"`
	Viewers                  int    `json:"viewers"`
}

func NewEventSubClient(db *DB, irc *IRCBot, tm *TokenManager, discord *DiscordWebhook, clientID, accessToken, broadcasterID, rewardID string) *EventSubClient {
	return &EventSubClient{
		db:            db,
		irc:           irc,
		tm:            tm,
		discord:       discord,
		clientID:      clientID,
		accessToken:   accessToken,
		broadcasterID: broadcasterID,
		rewardID:      rewardID,
	}
}

func (e *EventSubClient) Run(ctx context.Context) error {
	for {
		err := e.connect(ctx)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		log.Printf("EventSub WebSocket disconnected: %v, reconnecting in 5s...", err)

		// Refresh token before reconnecting
		if newToken, err := e.tm.Refresh(); err != nil {
			log.Printf("EventSub token refresh failed: %v", err)
		} else {
			e.accessToken = newToken
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(5 * time.Second):
		}
	}
}

func (e *EventSubClient) connect(ctx context.Context) error {
	conn, _, err := websocket.DefaultDialer.DialContext(ctx, twitchEventSubWSURL, nil)
	if err != nil {
		return fmt.Errorf("dial: %w", err)
	}
	defer conn.Close()

	// Close connection on context cancel to unblock the reader
	go func() {
		<-ctx.Done()
		conn.Close()
	}()

	for {
		_, raw, err := conn.ReadMessage()
		if err != nil {
			return fmt.Errorf("read: %w", err)
		}

		var msg wsMessage
		if err := json.Unmarshal(raw, &msg); err != nil {
			log.Printf("unmarshal ws message: %v", err)
			continue
		}

		switch msg.Metadata.MessageType {
		case "session_welcome":
			var wp welcomePayload
			if err := json.Unmarshal(msg.Payload, &wp); err != nil {
				return fmt.Errorf("unmarshal welcome: %w", err)
			}
			e.sessionID = wp.Session.ID
			log.Printf("EventSub connected, session: %s", e.sessionID)

			if err := e.subscribe(); err != nil {
				return fmt.Errorf("subscribe: %w", err)
			}
			log.Println("Subscribed to channel point redemptions and raids")

		case "session_keepalive":
			// no-op, connection is alive

		case "notification":
			e.handleNotification(msg.Payload)

		case "session_reconnect":
			log.Println("Received reconnect, will reconnect...")
			return fmt.Errorf("reconnect requested")
		}
	}
}

func (e *EventSubClient) handleNotification(payload json.RawMessage) {
	var np notificationPayload
	if err := json.Unmarshal(payload, &np); err != nil {
		log.Printf("unmarshal notification: %v", err)
		return
	}

	switch np.Subscription.Type {
	case "channel.channel_points_custom_reward_redemption.add":
		var event redemptionEvent
		if err := json.Unmarshal(np.Event, &event); err != nil {
			log.Printf("unmarshal redemption event: %v", err)
			return
		}

		// Filter by reward ID if configured
		if e.rewardID != "" && event.Reward.ID != e.rewardID {
			return
		}

		log.Printf("FIRST redeemed by %s (%s)", event.UserName, event.UserID)

		if err := e.db.RecordFirst(event.UserID, event.UserName); err != nil {
			log.Printf("record first: %v", err)
			return
		}

		e.irc.Say(fmt.Sprintf("🏆 %s claimed FIRST!", event.UserName))
		e.irc.SayLeaderboard()

		rank, count, err := e.db.UserRank(event.UserName)
		if err != nil {
			log.Printf("user rank lookup: %v", err)
		} else {
			go e.discord.LogFirst(event.UserName, rank, count, time.Now())
		}

	case "channel.raid":
		var event raidEvent
		if err := json.Unmarshal(np.Event, &event); err != nil {
			log.Printf("unmarshal raid event: %v", err)
			return
		}

		log.Printf("Raided by %s with %d viewers", event.FromBroadcasterUserName, event.Viewers)
		e.irc.Say(fmt.Sprintf("/shoutout %s", event.FromBroadcasterUserLogin))
	}
}

func (e *EventSubClient) subscribe() error {
	type condition map[string]string
	type transport struct {
		Method    string `json:"method"`
		SessionID string `json:"session_id"`
	}
	type subRequest struct {
		Type      string    `json:"type"`
		Version   string    `json:"version"`
		Condition condition `json:"condition"`
		Transport transport `json:"transport"`
	}

	tp := transport{
		Method:    "websocket",
		SessionID: e.sessionID,
	}

	// Subscribe to channel point redemptions
	redemptionCond := condition{"broadcaster_user_id": e.broadcasterID}
	if e.rewardID != "" {
		redemptionCond["reward_id"] = e.rewardID
	}
	if err := twitchAPIPost("https://api.twitch.tv/helix/eventsub/subscriptions", e.clientID, e.accessToken, subRequest{
		Type:      "channel.channel_points_custom_reward_redemption.add",
		Version:   "1",
		Condition: redemptionCond,
		Transport: tp,
	}); err != nil {
		return fmt.Errorf("subscribe redemptions: %w", err)
	}

	// Subscribe to raids
	if err := twitchAPIPost("https://api.twitch.tv/helix/eventsub/subscriptions", e.clientID, e.accessToken, subRequest{
		Type:      "channel.raid",
		Version:   "1",
		Condition: condition{"to_broadcaster_user_id": e.broadcasterID},
		Transport: tp,
	}); err != nil {
		return fmt.Errorf("subscribe raids: %w", err)
	}

	return nil
}
