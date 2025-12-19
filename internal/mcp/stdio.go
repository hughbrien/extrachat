package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os/exec"
	"sync"
	"sync/atomic"
)

// StdioClient implements MCPClient for local Python MCP servers via stdio
type StdioClient struct {
	name    string
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	stdout  io.ReadCloser
	stderr  io.ReadCloser
	scanner *bufio.Scanner
	reqID   int32
	logger  *slog.Logger
	mu      sync.Mutex
	closed  bool
}

// NewStdioClient creates a new stdio-based MCP client for local Python servers
func NewStdioClient(name string, pythonScript string, logger *slog.Logger) (*StdioClient, error) {
	if logger == nil {
		return nil, fmt.Errorf("logger cannot be nil")
	}

	// Start Python MCP server process
	cmd := exec.Command("python3", pythonScript)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to create stdin pipe: %w", err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		stdin.Close()
		return nil, fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		stdin.Close()
		stdout.Close()
		return nil, fmt.Errorf("failed to create stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		stdin.Close()
		stdout.Close()
		stderr.Close()
		return nil, fmt.Errorf("failed to start Python process: %w", err)
	}

	client := &StdioClient{
		name:    name,
		cmd:     cmd,
		stdin:   stdin,
		stdout:  stdout,
		stderr:  stderr,
		scanner: bufio.NewScanner(stdout),
		reqID:   0,
		logger:  logger,
		closed:  false,
	}

	// Start goroutine to log stderr
	go client.logStderr()

	logger.Info("started MCP stdio client", "name", name, "script", pythonScript)

	return client, nil
}

// Name returns the client identifier
func (c *StdioClient) Name() string {
	return c.name
}

// Initialize establishes connection to MCP server
func (c *StdioClient) Initialize(ctx context.Context) error {
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
func (c *StdioClient) ListTools(ctx context.Context) ([]Tool, error) {
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
func (c *StdioClient) CallTool(ctx context.Context, toolName string, args map[string]interface{}) (interface{}, error) {
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
func (c *StdioClient) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed {
		return nil
	}
	c.closed = true

	// Close pipes
	if c.stdin != nil {
		c.stdin.Close()
	}
	if c.stdout != nil {
		c.stdout.Close()
	}
	if c.stderr != nil {
		c.stderr.Close()
	}

	// Kill process
	if c.cmd != nil && c.cmd.Process != nil {
		if err := c.cmd.Process.Kill(); err != nil {
			c.logger.Warn("failed to kill MCP server process", "error", err)
		}
		c.cmd.Wait() // Clean up zombie process
	}

	c.logger.Info("closed MCP stdio client", "name", c.name)
	return nil
}

// sendRequest sends a JSON-RPC request and waits for response
func (c *StdioClient) sendRequest(ctx context.Context, method string, params interface{}, result interface{}) error {
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

	// Marshal request to JSON
	requestJSON, err := json.Marshal(request)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	// Send request
	if _, err := c.stdin.Write(append(requestJSON, '\n')); err != nil {
		return fmt.Errorf("failed to write request: %w", err)
	}

	// Read response
	if !c.scanner.Scan() {
		if err := c.scanner.Err(); err != nil {
			return fmt.Errorf("failed to read response: %w", err)
		}
		return fmt.Errorf("EOF from MCP server")
	}

	responseJSON := c.scanner.Bytes()

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

// logStderr logs stderr output from the Python process
func (c *StdioClient) logStderr() {
	scanner := bufio.NewScanner(c.stderr)
	for scanner.Scan() {
		c.logger.Warn("MCP server stderr", "server", c.name, "message", scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		c.logger.Error("error reading stderr", "server", c.name, "error", err)
	}
}
