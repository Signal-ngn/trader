package api

import (
	"sync"
	"testing"
	"time"
)

// ── Subscribe / Publish / Unsubscribe ─────────────────────────────────────────

func TestStreamRegistry_PublishReachesSubscriber(t *testing.T) {
	r := NewStreamRegistry()
	ch, unsubscribe := r.Subscribe("paper")
	defer unsubscribe()

	r.Publish("paper", map[string]string{"trade_id": "t1"})

	select {
	case data := <-ch:
		if len(data) == 0 {
			t.Fatal("received empty payload")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for published event")
	}
}

func TestStreamRegistry_PublishToWrongAccountNotReceived(t *testing.T) {
	r := NewStreamRegistry()
	ch, unsubscribe := r.Subscribe("paper")
	defer unsubscribe()

	r.Publish("live", map[string]string{"trade_id": "t1"})

	select {
	case <-ch:
		t.Fatal("should not receive event for a different account")
	case <-time.After(50 * time.Millisecond):
		// correct — nothing received
	}
}

func TestStreamRegistry_MultipleSubscribersSameAccount(t *testing.T) {
	r := NewStreamRegistry()
	ch1, unsub1 := r.Subscribe("paper")
	ch2, unsub2 := r.Subscribe("paper")
	defer unsub1()
	defer unsub2()

	r.Publish("paper", map[string]string{"trade_id": "t1"})

	for i, ch := range []<-chan []byte{ch1, ch2} {
		select {
		case data := <-ch:
			if len(data) == 0 {
				t.Fatalf("subscriber %d received empty payload", i+1)
			}
		case <-time.After(time.Second):
			t.Fatalf("subscriber %d timed out", i+1)
		}
	}
}

func TestStreamRegistry_UnsubscribeStopsDelivery(t *testing.T) {
	r := NewStreamRegistry()
	ch, unsubscribe := r.Subscribe("paper")

	// Publish before unsubscribing — should be received.
	r.Publish("paper", map[string]string{"trade_id": "t1"})
	select {
	case data := <-ch:
		if len(data) == 0 {
			t.Fatal("expected non-empty data before unsubscribe")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for pre-unsubscribe event")
	}

	unsubscribe()

	// After unsubscribe, subsequent publishes should not reach this channel.
	r.Publish("paper", map[string]string{"trade_id": "t2"})
	select {
	case <-ch:
		t.Fatal("should not receive event after unsubscribe")
	case <-time.After(50 * time.Millisecond):
		// correct — nothing delivered
	}

	// Publish after unsubscribe should not panic.
	r.Publish("paper", map[string]string{"trade_id": "t3"})
}

func TestStreamRegistry_NoSubscribersDropsSilently(t *testing.T) {
	r := NewStreamRegistry()
	// Should not panic with no subscribers registered.
	r.Publish("paper", map[string]string{"trade_id": "t1"})
}

func TestStreamRegistry_SlowSubscriberEventDropped(t *testing.T) {
	r := NewStreamRegistry()
	// Buffer is 16; fill it entirely then publish one more — should not block.
	_, unsubscribe := r.Subscribe("paper")
	defer unsubscribe()

	payload := map[string]string{"trade_id": "tx"}
	for i := 0; i < 16; i++ {
		r.Publish("paper", payload)
	}
	// Channel is now full. This 17th publish should be dropped, not block.
	done := make(chan struct{})
	go func() {
		r.Publish("paper", payload)
		close(done)
	}()

	select {
	case <-done:
		// correct — dropped without blocking
	case <-time.After(time.Second):
		t.Fatal("Publish blocked on a full subscriber channel")
	}
}

func TestStreamRegistry_ConcurrentPublishSubscribe(t *testing.T) {
	r := NewStreamRegistry()
	const goroutines = 20
	const publishes = 50

	var wg sync.WaitGroup
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ch, unsub := r.Subscribe("paper")
			defer unsub()
			for j := 0; j < publishes; j++ {
				r.Publish("paper", map[string]string{"x": "y"})
				// Drain to avoid blocking the goroutine.
				select {
				case <-ch:
				default:
				}
			}
		}()
	}
	// Should complete without deadlock or panic.
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("concurrent publish/subscribe test timed out")
	}
}
