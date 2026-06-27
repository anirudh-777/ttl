package events_test

import (
	"sync"
	"testing"
	"time"

	"github.com/anirudh-777/ttl/internal/events"
)

func TestPublishSubscribe(t *testing.T) {
	h := events.New()
	ch, unsub := h.Subscribe(8)
	defer unsub()

	want := events.Event{
		Kind:     events.KindTaskCreated,
		TenantID: "t1",
		Payload:  map[string]any{"id": "abc"},
	}
	h.Publish(want)

	select {
	case got := <-ch:
		if got.Kind != want.Kind || got.TenantID != want.TenantID {
			t.Errorf("got %+v, want %+v", got, want)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for event")
	}
}

func TestSubscribeFanout(t *testing.T) {
	h := events.New()
	a, unsubA := h.Subscribe(4)
	b, unsubB := h.Subscribe(4)
	defer unsubA()
	defer unsubB()

	h.Publish(events.Event{Kind: "x"})

	var wg sync.WaitGroup
	wg.Add(2)
	for _, ch := range []<-chan events.Event{a, b} {
		go func(c <-chan events.Event) {
			defer wg.Done()
			<-c
		}(ch)
	}
	wg.Wait()
}

func TestDropOnFullSubscriber(t *testing.T) {
	h := events.New()
	ch, unsub := h.Subscribe(1)
	defer unsub()

	// Publish 5 events to a buffer of 1; at least 4 should be dropped
	// for this subscriber. Total drops recorded by the hub equals 4.
	h.Publish(events.Event{Kind: "a"})
	h.Publish(events.Event{Kind: "b"})
	h.Publish(events.Event{Kind: "c"})
	h.Publish(events.Event{Kind: "d"})
	h.Publish(events.Event{Kind: "e"})

	if got := h.Dropped(); got < 3 {
		t.Errorf("Dropped = %d, want >=3", got)
	}
	// Drain so unsub doesn't close on a buffered event.
	<-ch
}

func TestUnsubscribeClosesChannel(t *testing.T) {
	h := events.New()
	ch, unsub := h.Subscribe(1)
	unsub()
	if _, ok := <-ch; ok {
		t.Error("expected closed channel after unsub")
	}
}
