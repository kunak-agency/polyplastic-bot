package max

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestPollerHandlesInOrderAndAdvancesMarker(t *testing.T) {
	f := NewFake()
	f.Push(Update{Type: UpdateBotStarted, UserID: 1}, Update{Type: UpdateMessageCreated, ChatID: 2, Text: "a"})
	f.PushError(errors.New("сеть моргнула"))
	f.Push(Update{Type: UpdateMessageCreated, ChatID: 2, Text: "b"})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var mu sync.Mutex
	var got []string
	var markers []int64
	done := make(chan struct{})
	p := &Poller{
		Client:  f,
		Backoff: time.Millisecond,
		Handle: func(_ context.Context, u Update) {
			mu.Lock()
			defer mu.Unlock()
			got = append(got, u.Type+":"+u.Text)
			if u.Text == "b" {
				close(done)
			}
		},
		OnMarker: func(m int64) { mu.Lock(); markers = append(markers, m); mu.Unlock() },
	}
	errc := make(chan error, 1)
	go func() { errc <- p.Run(ctx) }()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("поллер не дошёл до третьего события")
	}
	cancel()
	if err := <-errc; !errors.Is(err, context.Canceled) {
		t.Fatalf("Run вернул %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	want := []string{"bot_started:", "message_created:a", "message_created:b"}
	if len(got) != len(want) {
		t.Fatalf("события %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("события %v, want %v", got, want)
		}
	}
	if len(markers) < 1 || markers[0] != 2 {
		t.Fatalf("marker после первой пачки %v, want 2", markers)
	}
}

func TestPollerSurvivesPanic(t *testing.T) {
	f := NewFake()
	f.Push(Update{Type: "boom"}, Update{Type: UpdateMessageCreated, Text: "ok"})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	p := &Poller{Client: f, Handle: func(_ context.Context, u Update) {
		if u.Type == "boom" {
			panic("сломался обработчик")
		}
		close(done)
	}}
	go func() { _ = p.Run(ctx) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("после паники цикл остановился")
	}
}
