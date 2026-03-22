package events

import (
	"context"
	"testing"
	"time"
)

// TestSubscribeAndPublish verifies that a handler receives the published event.
func TestSubscribeAndPublish(t *testing.T) {
	bus := NewEventBus()
	received := make(chan Event, 1)

	bus.Subscribe(FileUploaded, func(e Event) {
		received <- e
	})

	event := Event{
		Type:   FileUploaded,
		FileID: 123,
		UserID: 456,
		Path:   "/test/file.note",
	}

	bus.Publish(context.Background(), event)

	select {
	case e := <-received:
		if e.FileID != 123 || e.UserID != 456 || e.Path != "/test/file.note" {
			t.Fatalf("event fields mismatch: got %+v", e)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for event")
	}
}

// TestMultipleSubscribers verifies that all handlers for an event type are called.
func TestMultipleSubscribers(t *testing.T) {
	bus := NewEventBus()
	received1 := make(chan Event, 1)
	received2 := make(chan Event, 1)

	bus.Subscribe(FileUploaded, func(e Event) { received1 <- e })
	bus.Subscribe(FileUploaded, func(e Event) { received2 <- e })

	event := Event{
		Type:   FileUploaded,
		FileID: 789,
		UserID: 999,
		Path:   "/shared/document.note",
	}

	bus.Publish(context.Background(), event)

	// Both handlers should receive the event
	select {
	case <-received1:
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for first subscriber")
	}

	select {
	case <-received2:
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for second subscriber")
	}
}

// TestDifferentEventTypes verifies that handlers only receive their subscribed event type.
func TestDifferentEventTypes(t *testing.T) {
	bus := NewEventBus()
	uploadedReceived := make(chan Event, 1)
	deletedReceived := make(chan Event, 1)

	bus.Subscribe(FileUploaded, func(e Event) { uploadedReceived <- e })
	bus.Subscribe(FileDeleted, func(e Event) { deletedReceived <- e })

	// Publish a FileUploaded event
	bus.Publish(context.Background(), Event{
		Type:   FileUploaded,
		FileID: 100,
		UserID: 200,
		Path:   "/test/upload.note",
	})

	// Uploaded handler should receive, deleted should timeout
	select {
	case <-uploadedReceived:
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for upload event")
	}

	select {
	case <-deletedReceived:
		t.Fatal("deleted handler should not receive upload event")
	case <-time.After(100 * time.Millisecond):
		// Expected: no event
	}
}

// TestPublishWithNoSubscribers verifies that publishing with no subscribers doesn't panic.
func TestPublishWithNoSubscribers(t *testing.T) {
	bus := NewEventBus()

	// Should not panic
	bus.Publish(context.Background(), Event{
		Type:   FileModified,
		FileID: 555,
		UserID: 666,
		Path:   "/test/modified.note",
	})
}

// TestConcurrentPublish verifies that multiple goroutines publishing simultaneously all deliver events.
func TestConcurrentPublish(t *testing.T) {
	bus := NewEventBus()
	received := make(chan Event, 10)

	bus.Subscribe(FileUploaded, func(e Event) { received <- e })

	// Publish from 10 concurrent goroutines
	go func() {
		for i := 1; i <= 10; i++ {
			bus.Publish(context.Background(), Event{
				Type:   FileUploaded,
				FileID: int64(i),
				UserID: int64(i * 100),
				Path:   "/test/file.note",
			})
		}
	}()

	// Collect all 10 events
	count := 0
	deadline := time.Now().Add(2 * time.Second)
	for count < 10 {
		select {
		case <-received:
			count++
		case <-time.After(time.Until(deadline)):
			t.Fatalf("timeout: only received %d/%d events", count, 10)
		}
	}
}

// TestHandlerPanic verifies that handler panics don't crash the bus or affect other handlers.
func TestHandlerPanic(t *testing.T) {
	bus := NewEventBus()
	received := make(chan Event, 1)

	// First handler panics
	bus.Subscribe(FileUploaded, func(e Event) {
		panic("handler panic test")
	})

	// Second handler should still execute
	bus.Subscribe(FileUploaded, func(e Event) {
		received <- e
	})

	event := Event{
		Type:   FileUploaded,
		FileID: 111,
		UserID: 222,
		Path:   "/test/panic.note",
	}

	// Publish the event (panicking handler should not affect second handler)
	bus.Publish(context.Background(), event)

	select {
	case <-received:
		// Success: second handler executed despite first handler's panic
	case <-time.After(time.Second):
		t.Fatal("timeout: second handler did not execute after first handler panicked")
	}
}
