package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync/atomic"
)

// HTTPClient implements MCPClient for remote MCP servers via HTTP
type HTTPClient struct {
	name       string
	baseURL    string
	httpClient *http.Client
	reqID      int32
	logger     *slog.Logger
}

// NewHTTPClient creates a new HTTP-based MCP client for remote servers
func NewHTTPClient(name string, baseURL string, logger *slog.Logger) (*HTTPClient, error) {
	if logger == nil {
		return nil, fmt.Errorf("logger cannot be nil")
	}

	client := &HTTPClient{
		name:    name,
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 0, // No timeout for SSE streams
		},
		reqID:  0,
		logger: logger,
	}

	logger.Info("created MCP HTTP client", "name", name, "url", baseURL)
	return client, nil
}

// Name returns the client identifier
func (c *HTTPClient) Name() string {
	return c.name
}

// Initialize establishes connection to MCP server
func (c *HTTPClient) Initialize(ctx context.Context) error {
	params := InitializeParams{
		ClientInfo: ClientInfo{
			Name:    "extrachat",
			Version: "1.0.0",
		},
	}

	var result InitializeResult
	if err := c.sendRequest(ctx, MethodInitialize, params, &result); err != nil {
		return fmt.Errorf("initialize failed: %w", err)
	}

	c.logger.Info("MCP server initialized", "server", result.ServerInfo.Name, "version", result.ServerInfo.Version)
	return nil
}

// ListTools returns available tools from this MCP server
func (c *HTTPClient) ListTools(ctx context.Context) ([]Tool, error) {
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
func (c *HTTPClient) CallTool(ctx context.Context, toolName string, args map[string]interface{}) (interface{}, error) {
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
func (c *HTTPClient) Close() error {
	c.logger.Info("closed MCP HTTP client", "name", c.name)
	return nil
}

// sendRequest sends an HTTP JSON-RPC request
func (c *HTTPClient) sendRequest(ctx context.Context, method string, params interface{}, result interface{}) error {
	// Generate unique request ID
	reqID := int(atomic.AddInt32(&c.reqID, 1))

	// Build JSON-RPC request
	request := JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      reqID,
		Method:  method,
		Params:  params,
	}

	// Marshal request to JSON
	requestJSON, err := json.Marshal(request)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	// Create HTTP request
	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/rpc", bytes.NewBuffer(requestJSON))
	if err != nil {
		return fmt.Errorf("failed to create HTTP request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")

	// Send request
	httpResp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("failed to send HTTP request: %w", err)
	}
	defer httpResp.Body.Close()

	// Check HTTP status
	if httpResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(httpResp.Body)
		return fmt.Errorf("HTTP error %d: %s", httpResp.StatusCode, string(body))
	}

	// Read response body
	responseJSON, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response: %w", err)
	}

	// Parse JSON-RPC response
	var response JSONRPCResponse
	if err := json.Unmarshal(responseJSON, &response); err != nil {
		return fmt.Errorf("failed to unmarshal response: %w", err)
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
