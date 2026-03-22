package events

import (
	"context"
	"log/slog"
	"sync"
)

// EventBus is an in-process pub/sub event bus.
// Handlers are dispatched in separate goroutines and publishing never blocks.
type EventBus struct {
	mu       sync.RWMutex
	handlers map[string][]func(Event)
}

// NewEventBus creates a new event bus.
func NewEventBus() *EventBus {
	return &EventBus{
		handlers: make(map[string][]func(Event)),
	}
}

// Subscribe registers a handler for a specific event type.
// The handler will be called in a separate goroutine for each published event.
func (b *EventBus) Subscribe(eventType string, handler func(Event)) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.handlers[eventType] = append(b.handlers[eventType], handler)
}

// Publish dispatches an event to all registered handlers for that event type.
// Handlers are called concurrently in separate goroutines.
// If a handler panics, the panic is logged but does not affect other handlers or the bus.
func (b *EventBus) Publish(ctx context.Context, event Event) {
	b.mu.RLock()
	handlersCopy := make([]func(Event), len(b.handlers[event.Type]))
	copy(handlersCopy, b.handlers[event.Type])
	b.mu.RUnlock()

	for _, handler := range handlersCopy {
		// Dispatch in a separate goroutine (fire-and-forget)
		go func(h func(Event)) {
			defer func() {
				if r := recover(); r != nil {
					slog.Error("handler panic in event bus",
						"event_type", event.Type,
						"panic", r,
					)
				}
			}()
			h(event)
		}(handler)
	}
}
