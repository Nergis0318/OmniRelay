package hub

import (
	"encoding/json"
	"sync"
	"testing"
	"time"
)

func TestHubRegisterBroadcast(t *testing.T) {
	h := New()
	c := h.Register(1)

	event := Event{Type: "test", Data: json.RawMessage(`{"hello":"world"}`)}
	h.Broadcast(1, event)

	select {
	case got := <-c.events:
		var e Event
		if err := json.Unmarshal(got, &e); err != nil {
			t.Fatalf("failed to unmarshal event: %v", err)
		}
		if e.Type != "test" {
			t.Errorf("expected type 'test', got '%s'", e.Type)
		}
	default:
		t.Fatal("expected event on channel")
	}
}

func TestHubUserIsolation(t *testing.T) {
	h := New()
	c1 := h.Register(1)
	c2 := h.Register(2)

	h.Broadcast(1, Event{Type: "msg", Data: json.RawMessage(`"only for user 1"`)})

	select {
	case <-c1.events:
		// expected
	default:
		t.Fatal("user 1 should have received event")
	}

	select {
	case <-c2.events:
		t.Fatal("user 2 should NOT have received event")
	default:
	}
}

func TestHubMultiConn(t *testing.T) {
	h := New()
	c1 := h.Register(1)
	c2 := h.Register(1)

	h.Broadcast(1, Event{Type: "multi", Data: json.RawMessage(`true`)})

	received := 0
	select {
	case <-c1.events:
		received++
	default:
	}
	select {
	case <-c2.events:
		received++
	default:
	}
	if received != 2 {
		t.Fatalf("expected 2 deliveries, got %d", received)
	}

	_ = c1
	_ = c2
}

func TestHubUnregister(t *testing.T) {
	h := New()
	c := h.Register(1)
	h.Unregister(c)

	h.Broadcast(1, Event{Type: "gone", Data: json.RawMessage(`true`)})

	h.mu.Lock()
	conns := h.connections[1]
	h.mu.Unlock()
	if len(conns) != 0 {
		t.Fatalf("expected 0 connections after unregister, got %d", len(conns))
	}
}

func TestHubBroadcastDeadConn(t *testing.T) {
	h := New()
	c := h.Register(1)

	for i := 0; i < 20; i++ {
		h.Broadcast(1, Event{Type: "fill", Data: json.RawMessage(`"x"`)})
	}

	h.mu.Lock()
	conns := h.connections[1]
	h.mu.Unlock()

	if len(conns) != 0 {
		t.Fatalf("expected 0 connections after dead conn cleanup, got %d", len(conns))
	}
	_ = c
}

func TestHubConcurrentRegisterBroadcast(t *testing.T) {
	h := New()
	var wg sync.WaitGroup
	stop := make(chan struct{})
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c := h.Register(1)
			<-stop
			_ = c
		}()
	}
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ticker := time.NewTicker(time.Millisecond)
			defer ticker.Stop()
			for {
				select {
				case <-stop:
					return
				case <-ticker.C:
					h.Broadcast(1, Event{Type: "x", Data: json.RawMessage(`1`)})
				}
			}
		}()
	}
	time.Sleep(100 * time.Millisecond)
	close(stop)
	wg.Wait()
}
