package server

import (
	"context"
	"testing"
	"time"
)

// quickGrace shortens the startup floor so a test does not have to sit through
// it. Every idle test that expects a shutdown needs this.
func quickGrace(t *testing.T) {
	t.Helper()
	restore := idleGrace
	idleGrace = time.Millisecond
	t.Cleanup(func() { idleGrace = restore })
}

func TestAnUnwatchedServerGivesUp(t *testing.T) {
	quickGrace(t)
	hub := newReloadHub()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Nothing else ends the process: closing the tab used to leave mdview and
	// its chat agent resident for weeks.
	stopped := make(chan struct{})
	go watchIdle(ctx, hub, time.Nanosecond, func() { close(stopped) })

	select {
	case <-stopped:
	case <-time.After(2 * time.Second):
		t.Fatal("an unwatched server stayed up")
	}
}

func TestAWatchedServerStaysUp(t *testing.T) {
	quickGrace(t)
	hub := newReloadHub()
	ch := hub.subscribe()
	defer hub.unsubscribe(ch)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	stopped := make(chan struct{})
	go watchIdle(ctx, hub, time.Nanosecond, func() { close(stopped) })

	// A browser is attached, so however long it sits there it is not idle.
	select {
	case <-stopped:
		t.Fatal("shut down with a browser still attached")
	case <-time.After(150 * time.Millisecond):
	}
	if got := hub.attached(); got != 1 {
		t.Fatalf("attached = %d, want 1", got)
	}
}

func TestZeroIdleStaysUpForever(t *testing.T) {
	hub := newReloadHub()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// For a terminal you are sitting in front of.
	stopped := make(chan struct{})
	go watchIdle(ctx, hub, 0, func() { close(stopped) })

	select {
	case <-stopped:
		t.Fatal("shut down despite --idle 0")
	case <-time.After(150 * time.Millisecond):
	}
}

func TestTheWatcherStopsWithTheServer(t *testing.T) {
	hub := newReloadHub()
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		watchIdle(ctx, hub, time.Hour, func() {})
		close(done)
	}()

	// It must not outlive what it is watching.
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("the idle watcher outlived the server")
	}
}

func TestAttachedCountsBrowsers(t *testing.T) {
	hub := newReloadHub()
	if got := hub.attached(); got != 0 {
		t.Fatalf("a fresh hub has %d clients, want 0", got)
	}

	one := hub.subscribe()
	two := hub.subscribe()
	if got := hub.attached(); got != 2 {
		t.Fatalf("attached = %d, want 2", got)
	}

	hub.unsubscribe(one)
	if got := hub.attached(); got != 1 {
		t.Fatalf("attached = %d after one tab closed, want 1", got)
	}

	hub.unsubscribe(two)
	if got := hub.attached(); got != 0 {
		t.Fatalf("a closed tab still counted as watching: attached = %d", got)
	}
}

func TestAServerLivesOutItsGracePeriod(t *testing.T) {
	hub := newReloadHub()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// A browser that is slow to open must not be raced by a short --idle.
	restore := idleGrace
	idleGrace = 200 * time.Millisecond
	defer func() { idleGrace = restore }()

	stopped := make(chan struct{})
	go watchIdle(ctx, hub, time.Millisecond, func() { close(stopped) })

	select {
	case <-stopped:
		t.Fatal("gave up before the browser had a chance to connect")
	case <-time.After(100 * time.Millisecond):
	}

	select {
	case <-stopped:
	case <-time.After(2 * time.Second):
		t.Fatal("never gave up after the grace period")
	}
}
