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
	nick     string
	token    string
	channel  string
	conn     net.Conn
	sendChan chan string
}

func NewIRCBot(db *DB, nick, token, channel string) *IRCBot {
	return &IRCBot{
		db:       db,
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

	// Close connection on context cancel to unblock the reader
	go func() {
		<-ctx.Done()
		conn.Close()
	}()

	// Writer goroutine
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case msg := <-b.sendChan:
				if err := send(msg); err != nil {
					log.Printf("irc send: %v", err)
					return
				}
			}
		}
	}()

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

func (b *IRCBot) handleMessage(line string) {
	// Parse PRIVMSG: :nick!user@host PRIVMSG #channel :message
	if !strings.Contains(line, "PRIVMSG") {
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

	entries, err := b.db.Leaderboard(5)
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
	medals := []string{"🥇", "🥈", "🥉", "4.", "5."}
	for i, e := range entries {
		if i > 0 {
			sb.WriteString(" | ")
		}
		fmt.Fprintf(&sb, "%s %s (%d)", medals[i], e.UserName, e.Count)
	}

	b.Say(sb.String())
}
