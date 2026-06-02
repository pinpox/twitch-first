package main

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"net"
	"strings"
	"time"
)

const twitchIRCAddr = "irc.chat.twitch.tv:6667"

type IRCBot struct {
	db       *DB
	tm       *TokenManager
	nick     string
	token    string
	channel  string
	conn     net.Conn
	sendChan chan string
}

func NewIRCBot(db *DB, tm *TokenManager, nick, token, channel string) *IRCBot {
	return &IRCBot{
		db:       db,
		tm:       tm,
		nick:     nick,
		token:    token,
		channel:  strings.ToLower(channel),
		sendChan: make(chan string, 64),
	}
}

func (b *IRCBot) Run(ctx context.Context) error {
	for {
		err := b.connect(ctx)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		log.Printf("IRC disconnected: %v, reconnecting in 5s...", err)

		// Refresh token before reconnecting
		if newToken, err := b.tm.Refresh(); err != nil {
			log.Printf("IRC token refresh failed: %v", err)
		} else {
			b.token = newToken
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(5 * time.Second):
		}
	}
}

func (b *IRCBot) Say(msg string) {
	b.sendChan <- fmt.Sprintf("PRIVMSG #%s :%s", b.channel, msg)
}

func (b *IRCBot) connect(ctx context.Context) error {
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", twitchIRCAddr)
	if err != nil {
		return fmt.Errorf("dial irc: %w", err)
	}
	b.conn = conn
	defer conn.Close()

	send := func(s string) error {
		_, err := fmt.Fprintf(conn, "%s\r\n", s)
		return err
	}

	if err := send("PASS oauth:" + b.token); err != nil {
		return err
	}
	if err := send("NICK " + b.nick); err != nil {
		return err
	}
	if err := send("JOIN #" + b.channel); err != nil {
		return err
	}

	log.Printf("IRC joined #%s", b.channel)

	// done is closed when this connection ends so the goroutines below stop
	// instead of outliving conn. Without it the writer would survive every
	// reconnect, accumulate across connections, and race other writers for
	// messages on the shared sendChan — a stale writer wins, fails to write to
	// its closed socket, and silently drops the message (only "irc send" logs).
	done := make(chan struct{})
	defer close(done)

	// Close the connection on context cancel (to unblock the reader); also stop
	// when this connection ends so the goroutine doesn't leak across reconnects.
	go func() {
		select {
		case <-ctx.Done():
			conn.Close()
		case <-done:
		}
	}()

	// Exactly one writer per connection, bound to this connection's lifetime.
	go b.writeLoop(ctx, send, done)

	scanner := bufio.NewScanner(conn)
	for scanner.Scan() {
		line := scanner.Text()

		if strings.HasPrefix(line, "PING") {
			if err := send("PONG" + line[4:]); err != nil {
				return fmt.Errorf("pong: %w", err)
			}
			continue
		}

		b.handleMessage(line)
	}
	return scanner.Err()
}

// writeLoop drains queued chat messages onto a single connection via send. It
// returns when the context is cancelled, when done is closed (the connection
// ended), or when a write fails. Binding it to done is what stops a writer from
// surviving a reconnect and stealing messages off sendChan that it can no
// longer deliver.
func (b *IRCBot) writeLoop(ctx context.Context, send func(string) error, done <-chan struct{}) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-done:
			return
		case msg := <-b.sendChan:
			if err := send(msg); err != nil {
				log.Printf("irc send: %v", err)
				return
			}
		}
	}
}

func (b *IRCBot) SayLeaderboard() {
	entries, err := b.db.Leaderboard(10)
	if err != nil {
		log.Printf("leaderboard query: %v", err)
		return
	}

	if len(entries) == 0 {
		b.Say("No one has claimed FIRST yet!")
		return
	}

	var sb strings.Builder
	sb.WriteString("FIRST leaderboard: ")
	medals := []string{"🥇", "🥈", "🥉", "4.", "5.", "6.", "7.", "8.", "9.", "10."}
	for i, e := range entries {
		if i > 0 {
			sb.WriteString(" | ")
		}
		fmt.Fprintf(&sb, "%s %s (%d)", medals[i], e.UserName, e.Count)
	}

	b.Say(sb.String())
}

// SayUserRank announces the given user's rank and FIRST count.
// If the user has no recorded FIRSTs, it says so.
func (b *IRCBot) SayUserRank(userName string) {
	rank, count, err := b.db.UserRank(userName)
	if err != nil {
		log.Printf("user rank query: %v", err)
		return
	}
	if count == 0 {
		b.Say(fmt.Sprintf("@%s you haven't claimed FIRST yet — get in there!", userName))
		return
	}
	suffix := "s"
	if count == 1 {
		suffix = ""
	}
	b.Say(fmt.Sprintf("@%s you are #%d with %d FIRST%s", userName, rank, count, suffix))
}

func (b *IRCBot) handleMessage(line string) {
	// Parse PRIVMSG: :nick!user@host PRIVMSG #channel :message
	if !strings.Contains(line, "PRIVMSG") {
		return
	}

	prefix, _, ok := strings.Cut(line, " ")
	if !ok || !strings.HasPrefix(prefix, ":") {
		return
	}

	parts := strings.SplitN(line, " :", 2)
	if len(parts) < 2 {
		return
	}
	msg := strings.TrimSpace(parts[1])

	if !strings.EqualFold(msg, "!first") {
		return
	}

	// Extract nick from prefix `:nick!user@host`
	bang := strings.Index(prefix, "!")
	if bang < 2 {
		return
	}
	nick := prefix[1:bang]

	b.SayLeaderboard()
	b.SayUserRank(nick)
}
