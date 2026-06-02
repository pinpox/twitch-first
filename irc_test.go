package main

import (
	"context"
	"sync"
	"testing"
	"time"
)

// TestWriteLoopStopsWhenConnectionEnds is the regression test for the stale-
// writer bug: a writer goroutine must not outlive its connection. Before the
// fix, every reconnect left its writer alive on the shared sendChan; a stale
// writer would win a queued message, fail to write it to its closed socket, and
// silently drop it (only an "irc send" line logged). The journal showed exactly
// this: a redemption's two Say calls landing on two dead sockets.
//
// Once the connection's done channel is closed, writeLoop must return and leave
// any later message buffered in sendChan for the next connection's writer.
func TestWriteLoopStopsWhenConnectionEnds(t *testing.T) {
	b := NewIRCBot(nil, nil, "bot", "tok", "Channel")

	var mu sync.Mutex
	var sent []string
	send := func(s string) error {
		mu.Lock()
		sent = append(sent, s)
		mu.Unlock()
		return nil
	}
	sentLen := func() int {
		mu.Lock()
		defer mu.Unlock()
		return len(sent)
	}

	done := make(chan struct{})
	finished := make(chan struct{})
	go func() {
		b.writeLoop(context.Background(), send, done)
		close(finished)
	}()

	// While the connection is up, queued messages are delivered.
	b.Say("up")
	deadline := time.Now().Add(time.Second)
	for sentLen() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if got := sentLen(); got != 1 {
		t.Fatalf("message not delivered while connection up: sent %d, want 1", got)
	}

	// The connection ends: the writer must stop. Waiting on finished makes this
	// deterministic — no message is queued during this window, so the select
	// must take the done branch.
	close(done)
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("writeLoop did not stop after its connection ended")
	}

	// A message queued after the connection ended must not be consumed by the
	// stopped writer; it stays buffered for the next connection's writer.
	b.Say("after")
	if got := sentLen(); got != 1 {
		t.Fatalf("writer sent %d messages, want 1 (it consumed a post-disconnect message)", got)
	}
	select {
	case m := <-b.sendChan:
		if want := "PRIVMSG #channel :after"; m != want {
			t.Fatalf("buffered message = %q, want %q", m, want)
		}
	default:
		t.Fatal("post-disconnect message was dropped, not buffered for the next writer")
	}
}
