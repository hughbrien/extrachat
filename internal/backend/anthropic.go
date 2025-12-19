package backend

// AnthropicRequest represents the request body for Anthropic API
type AnthropicRequest struct {
	Model     string                   `json:"model"`
	MaxTokens int                      `json:"max_tokens"`
	Messages  []AnthropicMessage       `json:"messages"`
	Tools     []AnthropicTool          `json:"tools,omitempty"`
}

// AnthropicMessage represents a message in the conversation
type AnthropicMessage struct {
	Role    string                 `json:"role"`
	Content interface{}            `json:"content"` // Can be string or []AnthropicContent
}

// AnthropicContent represents different content types (text, tool_use, tool_result)
type AnthropicContent struct {
	Type       string                 `json:"type"`
	Text       string                 `json:"text,omitempty"`
	ID         string                 `json:"id,omitempty"`          // For tool_use
	Name       string                 `json:"name,omitempty"`        // For tool_use
	Input      map[string]interface{} `json:"input,omitempty"`       // For tool_use
	ToolUseID  string                 `json:"tool_use_id,omitempty"` // For tool_result
	Content    interface{}            `json:"content,omitempty"`     // For tool_result (string or array)
	IsError    bool                   `json:"is_error,omitempty"`    // For tool_result
}

// AnthropicTool represents a tool definition
type AnthropicTool struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	InputSchema map[string]interface{} `json:"input_schema"`
}

// AnthropicResponse represents the response from Anthropic API
type AnthropicResponse struct {
	ID           string                 `json:"id"`
	Type         string                 `json:"type"`
	Role         string                 `json:"role"`
	Content      []AnthropicContent     `json:"content"`
	Model        string                 `json:"model"`
	StopReason   string                 `json:"stop_reason"`
	StopSequence string                 `json:"stop_sequence"`
	Usage        map[string]interface{} `json:"usage"`
}
