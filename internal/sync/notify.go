package sync

import (
	"sync"
)

// wsClient represents a connected WebSocket client.
type wsClient struct {
	userID     int64
	deviceType string
	send       chan string // buffered, size 16
	done       chan struct{}
}

// NotifyManager manages connected WebSocket clients and broadcasts messages to them.
type NotifyManager struct {
	mu      sync.RWMutex
	clients map[int64][]*wsClient // key: userID
}

// NewNotifyManager creates a new NotifyManager.
func NewNotifyManager() *NotifyManager {
	return &NotifyManager{
		clients: make(map[int64][]*wsClient),
	}
}

// Register adds a client to the registry for a specific user.
func (nm *NotifyManager) Register(client *wsClient) {
	nm.mu.Lock()
	defer nm.mu.Unlock()
	nm.clients[client.userID] = append(nm.clients[client.userID], client)
}

// Unregister removes a client from the registry and closes its done channel.
func (nm *NotifyManager) Unregister(client *wsClient) {
	nm.mu.Lock()
	defer nm.mu.Unlock()

	clients := nm.clients[client.userID]
	for i, c := range clients {
		if c == client {
			// Remove from slice
			nm.clients[client.userID] = append(clients[:i], clients[i+1:]...)
			break
		}
	}

	// Close the done channel to signal the client to stop
	close(client.done)
}

// NotifyUser sends a message to all clients for a specific user.
// Non-blocking: skips sending if the client's send channel is full.
func (nm *NotifyManager) NotifyUser(userID int64, data string) {
	nm.mu.RLock()
	clients := nm.clients[userID]
	// Make a copy to avoid holding the lock while sending
	clientsCopy := make([]*wsClient, len(clients))
	copy(clientsCopy, clients)
	nm.mu.RUnlock()

	for _, client := range clientsCopy {
		// Non-blocking send
		select {
		case client.send <- data:
		default:
			// Channel full, skip this client
		}
	}
}

// NotifyAll sends a message to all connected clients.
// Non-blocking: skips clients with full send channels.
func (nm *NotifyManager) NotifyAll(data string) {
	nm.mu.RLock()
	// Collect all clients
	var allClients []*wsClient
	for _, clients := range nm.clients {
		allClients = append(allClients, clients...)
	}
	nm.mu.RUnlock()

	for _, client := range allClients {
		// Non-blocking send
		select {
		case client.send <- data:
		default:
			// Channel full, skip this client
		}
	}
}
