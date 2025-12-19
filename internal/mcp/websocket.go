package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"

	"github.com/gorilla/websocket"
)

// WebSocketClient implements MCPClient for remote MCP servers via WebSocket
type WebSocketClient struct {
	name   string
	url    string
	conn   *websocket.Conn
	reqID  int32
	logger *slog.Logger
	mu     sync.Mutex
	closed bool
}

// NewWebSocketClient creates a new WebSocket-based MCP client for remote servers
func NewWebSocketClient(name string, url string, logger *slog.Logger) (*WebSocketClient, error) {
	if logger == nil {
		return nil, fmt.Errorf("logger cannot be nil")
	}

	// Connect to WebSocket server
	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to WebSocket: %w", err)
	}

	client := &WebSocketClient{
		name:   name,
		url:    url,
		conn:   conn,
		reqID:  0,
		logger: logger,
		closed: false,
	}

	logger.Info("created MCP WebSocket client", "name", name, "url", url)
	return client, nil
}

// Name returns the client identifier
func (c *WebSocketClient) Name() string {
	return c.name
}

// Initialize establishes connection to MCP server
func (c *WebSocketClient) Initialize(ctx context.Context) error {
	params := InitializeParams{
		ProtocolVersion: "2024-11-05",
		Capabilities: ClientCapabilities{
			Roots: &RootsCapability{
				ListChanged: false,
			},
		},
		ClientInfo: ClientInfo{
			Name:    "extrachat",
			Version: "1.1.0",
		},
	}

	var result InitializeResult
	if err := c.sendRequest(ctx, MethodInitialize, params, &result); err != nil {
		return fmt.Errorf("initialize failed: %w", err)
	}

	c.logger.Info("MCP server initialized",
		"server", result.ServerInfo.Name,
		"version", result.ServerInfo.Version,
		"protocol", result.ProtocolVersion)
	return nil
}

// ListTools returns available tools from this MCP server
func (c *WebSocketClient) ListTools(ctx context.Context) ([]Tool, error) {
	var result ListToolsResult
	if err := c.sendRequest(ctx, MethodListTools, nil, &result); err != nil {
		return nil, fmt.Errorf("list tools failed: %w", err)
	}

	tools := make([]Tool, len(result.Tools))
	for i, toolInfo := range result.Tools {
		tools[i] = Tool{
			Name:        toolInfo.Name,
			Description: toolInfo.Description,
			InputSchema: toolInfo.InputSchema,
			ServerName:  c.name,
		}
	}

	c.logger.Info("listed tools from MCP server", "server", c.name, "count", len(tools))
	return tools, nil
}

// CallTool invokes a tool with given arguments
func (c *WebSocketClient) CallTool(ctx context.Context, toolName string, args map[string]interface{}) (interface{}, error) {
	params := CallToolParams{
		Name:      toolName,
		Arguments: args,
	}

	var result CallToolResult
	if err := c.sendRequest(ctx, MethodCallTool, params, &result); err != nil {
		return nil, fmt.Errorf("call tool failed: %w", err)
	}

	c.logger.Info("called tool", "server", c.name, "tool", toolName)
	return result, nil
}

// Close disconnects from the MCP server
func (c *WebSocketClient) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed {
		return nil
	}
	c.closed = true

	if c.conn != nil {
		// Send close message
		c.conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
		c.conn.Close()
	}

	c.logger.Info("closed MCP WebSocket client", "name", c.name)
	return nil
}

// sendRequest sends a JSON-RPC request over WebSocket
func (c *WebSocketClient) sendRequest(ctx context.Context, method string, params interface{}, result interface{}) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed {
		return fmt.Errorf("client is closed")
	}

	// Generate unique request ID
	reqID := int(atomic.AddInt32(&c.reqID, 1))

	// Build JSON-RPC request
	request := JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      reqID,
		Method:  method,
		Params:  params,
	}

	// Send request
	if err := c.conn.WriteJSON(request); err != nil {
		return fmt.Errorf("failed to write request: %w", err)
	}

	// Read response
	var response JSONRPCResponse
	if err := c.conn.ReadJSON(&response); err != nil {
		return fmt.Errorf("failed to read response: %w", err)
	}

	// Check for JSON-RPC error
	if response.Error != nil {
		return fmt.Errorf("RPC error %d: %s", response.Error.Code, response.Error.Message)
	}

	// Unmarshal result into the provided result pointer
	if result != nil {
		resultJSON, err := json.Marshal(response.Result)
		if err != nil {
			return fmt.Errorf("failed to marshal result: %w", err)
		}
		if err := json.Unmarshal(resultJSON, result); err != nil {
			return fmt.Errorf("failed to unmarshal result: %w", err)
		}
	}

	return nil
}
