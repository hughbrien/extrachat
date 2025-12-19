package mcp

import (
	"context"
	"fmt"
	"sync"
)

// MCPClient represents a connection to an MCP server
type MCPClient interface {
	// Initialize establishes connection to MCP server
	Initialize(ctx context.Context) error

	// ListTools returns available tools from this MCP server
	ListTools(ctx context.Context) ([]Tool, error)

	// CallTool invokes a tool with given arguments
	CallTool(ctx context.Context, toolName string, args map[string]interface{}) (interface{}, error)

	// Close disconnects from the MCP server
	Close() error

	// Name returns the client identifier
	Name() string
}

// Tool represents an MCP tool/function available for invocation
type Tool struct {
	Name        string                 // Tool name
	Description string                 // Tool description
	InputSchema map[string]interface{} // JSON Schema for input parameters
	ServerName  string                 // Which server provides this tool
}

// ClientRegistry manages multiple MCP clients
type ClientRegistry struct {
	clients map[string]MCPClient
	mu      sync.RWMutex
}

// NewClientRegistry creates a new client registry
func NewClientRegistry() *ClientRegistry {
	return &ClientRegistry{
		clients: make(map[string]MCPClient),
	}
}

// Register adds a client to the registry
func (r *ClientRegistry) Register(name string, client MCPClient) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.clients[name] = client
}

// Get retrieves a client by name
func (r *ClientRegistry) Get(name string) (MCPClient, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	client, ok := r.clients[name]
	return client, ok
}

// All returns all registered clients
func (r *ClientRegistry) All() []MCPClient {
	r.mu.RLock()
	defer r.mu.RUnlock()
	clients := make([]MCPClient, 0, len(r.clients))
	for _, client := range r.clients {
		clients = append(clients, client)
	}
	return clients
}

// Close closes all registered clients
func (r *ClientRegistry) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	var firstErr error
	for name, client := range r.clients {
		if err := client.Close(); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("failed to close client %s: %w", name, err)
		}
	}
	return firstErr
}

// Count returns the number of registered clients
func (r *ClientRegistry) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.clients)
}
