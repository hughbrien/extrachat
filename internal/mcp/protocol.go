package mcp

// JSON-RPC 2.0 protocol types for Model Context Protocol

// JSONRPCRequest represents a JSON-RPC 2.0 request
type JSONRPCRequest struct {
	JSONRPC string      `json:"jsonrpc"` // Always "2.0"
	ID      int         `json:"id"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params,omitempty"`
}

// JSONRPCResponse represents a JSON-RPC 2.0 response
type JSONRPCResponse struct {
	JSONRPC string      `json:"jsonrpc"` // Always "2.0"
	ID      int         `json:"id"`
	Result  interface{} `json:"result,omitempty"`
	Error   *RPCError   `json:"error,omitempty"`
}

// RPCError represents a JSON-RPC 2.0 error
type RPCError struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

// MCP-specific JSON-RPC methods
const (
	MethodInitialize = "initialize"
	MethodListTools  = "tools/list"
	MethodCallTool   = "tools/call"
)

// InitializeParams represents parameters for initialize request
type InitializeParams struct {
	ClientInfo ClientInfo `json:"clientInfo"`
}

// ClientInfo contains client identification
type ClientInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// InitializeResult represents result from initialize request
type InitializeResult struct {
	ServerInfo ServerInfo `json:"serverInfo"`
}

// ServerInfo contains server identification
type ServerInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// ListToolsResult represents result from tools/list request
type ListToolsResult struct {
	Tools []ToolInfo `json:"tools"`
}

// ToolInfo describes an available tool
type ToolInfo struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	InputSchema map[string]interface{} `json:"inputSchema"` // JSON Schema
}

// CallToolParams represents parameters for tools/call request
type CallToolParams struct {
	Name      string                 `json:"name"`
	Arguments map[string]interface{} `json:"arguments"`
}

// CallToolResult represents result from tools/call request
type CallToolResult struct {
	Content []Content `json:"content"`
}

// Content represents tool response content
type Content struct {
	Type string `json:"type"` // e.g., "text"
	Text string `json:"text"`
}
