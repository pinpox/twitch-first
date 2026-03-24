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

func NewEventSubClient(db *DB, irc *IRCBot, clientID, accessToken, broadcasterID, rewardID string) *EventSubClient {
	return &EventSubClient{
		db:            db,
		irc:           irc,
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
			log.Println("Subscribed to channel point redemptions")

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

	if np.Subscription.Type != "channel.channel_points_custom_reward_redemption.add" {
		return
	}

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
}

func (e *EventSubClient) subscribe() error {
	type condition struct {
		BroadcasterUserID string `json:"broadcaster_user_id"`
		RewardID          string `json:"reward_id,omitempty"`
	}
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

	body := subRequest{
		Type:    "channel.channel_points_custom_reward_redemption.add",
		Version: "1",
		Condition: condition{
			BroadcasterUserID: e.broadcasterID,
			RewardID:          e.rewardID,
		},
		Transport: transport{
			Method:    "websocket",
			SessionID: e.sessionID,
		},
	}

	return twitchAPIPost("https://api.twitch.tv/helix/eventsub/subscriptions", e.clientID, e.accessToken, body)
}
