package chatbot

import (
	"bufio"
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"ExtraChat/internal/backend"
	"ExtraChat/internal/cache"
	"ExtraChat/internal/config"
	"ExtraChat/internal/mcp"
	"ExtraChat/internal/session"
	"ExtraChat/internal/telemetry"

	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

// ChatBot represents the main application
type ChatBot struct {
	config     config.Config
	db         *sql.DB
	cache      sync.Map
	logger     *slog.Logger
	tracer     trace.Tracer
	meter      metric.Meter
	httpClient *http.Client
	session    *session.Session
	mu         sync.Mutex

	// MCP support
	mcpRegistry *mcp.ClientRegistry // Registry of MCP clients
	mcpTools    []mcp.Tool           // Available tools from all MCP servers
}

// NewChatBot creates a new ChatBot instance
func NewChatBot(cfg config.Config) (*ChatBot, error) {
	logger, err := telemetry.InitLogger()
	if err != nil {
		return nil, fmt.Errorf("failed to initialize logger: %w", err)
	}

	ctx := context.Background()
	tracer, meter, _, err := telemetry.InitTelemetry(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize telemetry: %w", err)
	}

	db, err := telemetry.InitDB()
	if err != nil {
		return nil, fmt.Errorf("failed to initialize database: %w", err)
	}

	if cfg.Debug {
		logger.Info("Debug mode enabled")
	}

	cb := &ChatBot{
		config:     cfg,
		db:         db,
		logger:     logger,
		tracer:     tracer,
		meter:      meter,
		httpClient: &http.Client{Timeout: 60 * time.Second},
	}

	if cfg.SessionID != "" {
		sess, err := cb.loadSession(cfg.SessionID)
		if err != nil {
			logger.Warn("failed to load session, creating new one", "error", err)
			cb.session = cb.newSession()
		} else {
			cb.session = sess
			logger.Info("loaded existing session", "session_id", sess.ID)
		}
	} else {
		cb.session = cb.newSession()
	}

	// Initialize MCP if enabled
	if cfg.MCPEnabled {
		if err := cb.initializeMCP(); err != nil {
			logger.Warn("failed to initialize MCP, continuing without MCP support", "error", err)
		}
	}

	return cb, nil
}

// newSession creates a new session
func (cb *ChatBot) newSession() *session.Session {
	sessionID := fmt.Sprintf("session_%d", time.Now().Unix())
	sess := &session.Session{
		ID:        sessionID,
		StartTime: time.Now(),
		Backend:   cb.config.Backend,
		Messages:  []session.Message{},
	}
	cb.logger.Info("created new session", "session_id", sessionID, "backend", cb.config.Backend)
	return sess
}

// loadSession loads a session from the database
func (cb *ChatBot) loadSession(sessionID string) (*session.Session, error) {
	var backend string
	var startTime time.Time

	err := cb.db.QueryRow("SELECT backend, start_time FROM sessions WHERE id = ?", sessionID).
		Scan(&backend, &startTime)
	if err != nil {
		return nil, fmt.Errorf("session not found: %w", err)
	}

	rows, err := cb.db.Query(
		"SELECT role, content, timestamp FROM messages WHERE session_id = ? ORDER BY timestamp",
		sessionID,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to load messages: %w", err)
	}
	defer rows.Close()

	messages := []session.Message{}
	for rows.Next() {
		var msg session.Message
		if err := rows.Scan(&msg.Role, &msg.Content, &msg.Timestamp); err != nil {
			return nil, fmt.Errorf("failed to scan message: %w", err)
		}
		messages = append(messages, msg)
	}

	return &session.Session{
		ID:        sessionID,
		StartTime: startTime,
		Backend:   backend,
		Messages:  messages,
	}, nil
}

// saveSession saves the current session to the database
func (cb *ChatBot) saveSession() error {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	tx, err := cb.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	_, err = tx.Exec(
		"INSERT OR REPLACE INTO sessions (id, start_time, backend) VALUES (?, ?, ?)",
		cb.session.ID, cb.session.StartTime, cb.session.Backend,
	)
	if err != nil {
		return fmt.Errorf("failed to save session: %w", err)
	}

	for _, msg := range cb.session.Messages {
		_, err = tx.Exec(
			"INSERT INTO messages (session_id, role, content, timestamp) VALUES (?, ?, ?, ?)",
			cb.session.ID, msg.Role, msg.Content, msg.Timestamp,
		)
		if err != nil {
			cb.logger.Warn("failed to save message", "error", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	cb.logger.Info("session saved", "session_id", cb.session.ID, "message_count", len(cb.session.Messages))
	return nil
}

// checkCache checks if a response is cached
func (cb *ChatBot) checkCache(cacheKey string) (string, bool) {
	if val, ok := cb.cache.Load(cacheKey); ok {
		cached := val.(cache.CachedResponse)
		cb.logger.Info("cache hit", "key", cacheKey[:16])
		return cached.Response, true
	}
	return "", false
}

// storeCache stores a response in cache
func (cb *ChatBot) storeCache(cacheKey, response string) {
	cb.cache.Store(cacheKey, cache.CachedResponse{
		Response:  response,
		Timestamp: time.Now(),
	})
	cb.logger.Info("cached response", "key", cacheKey[:16])
}

// recordMetrics records OpenTelemetry metrics from usage data
func (cb *ChatBot) recordMetrics(ctx context.Context, usage map[string]interface{}) {
	if usage == nil {
		return
	}

	for key, value := range usage {
		if intVal, ok := value.(float64); ok {
			counter, err := cb.meter.Int64Counter(
				fmt.Sprintf("llm.usage.%s", key),
				metric.WithDescription(fmt.Sprintf("LLM usage metric: %s", key)),
			)
			if err != nil {
				cb.logger.Warn("failed to create counter", "key", key, "error", err)
				continue
			}
			counter.Add(ctx, int64(intVal))
		}
	}
}

// convertMCPToolsToAnthropic converts MCP tools to Anthropic tool format
func (cb *ChatBot) convertMCPToolsToAnthropic() []backend.AnthropicTool {
	tools := make([]backend.AnthropicTool, len(cb.mcpTools))
	for i, mcpTool := range cb.mcpTools {
		tools[i] = backend.AnthropicTool{
			Name:        mcpTool.Name,
			Description: mcpTool.Description,
			InputSchema: mcpTool.InputSchema,
		}
	}
	return tools
}

// callAnthropic calls the Anthropic API
func (cb *ChatBot) callAnthropic(ctx context.Context, messages []session.Message) (string, error) {
	ctx, span := cb.tracer.Start(ctx, "anthropic_api_call")
	defer span.End()

	start := time.Now()

	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		return "", fmt.Errorf("ANTHROPIC_API_KEY not set")
	}

	// Convert session messages to Anthropic message format
	reqMessages := make([]backend.AnthropicMessage, len(messages))
	for i, msg := range messages {
		reqMessages[i] = backend.AnthropicMessage{
			Role:    msg.Role,
			Content: msg.Content,
		}
	}

	// Build request with tools if MCP is enabled
	reqBody := backend.AnthropicRequest{
		Model:     "claude-sonnet-4-20250514",
		MaxTokens: 1024,
		Messages:  reqMessages,
	}

	// Add MCP tools if available
	if cb.config.MCPEnabled && len(cb.mcpTools) > 0 {
		reqBody.Tools = cb.convertMCPToolsToAnthropic()
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", "https://api.anthropic.com/v1/messages", bytes.NewBuffer(jsonData))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("x-api-key", apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")
	req.Header.Set("content-type", "application/json")

	resp, err := cb.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("API error: %s - %s", resp.Status, string(body))
	}

	var apiResp backend.AnthropicResponse
	if err := json.Unmarshal(body, &apiResp); err != nil {
		return "", fmt.Errorf("failed to unmarshal response: %w", err)
	}

	duration := time.Since(start)
	histogram, err := cb.meter.Float64Histogram(
		"http.client.request.duration",
		metric.WithDescription("HTTP request duration in milliseconds"),
	)
	if err == nil {
		histogram.Record(ctx, float64(duration.Milliseconds()))
	}

	cb.recordMetrics(ctx, apiResp.Usage)

	// Handle tool use
	if apiResp.StopReason == "tool_use" {
		return cb.handleAnthropicToolUse(ctx, messages, apiResp)
	}

	// Extract text response
	for _, content := range apiResp.Content {
		if content.Type == "text" {
			return content.Text, nil
		}
	}

	return "", fmt.Errorf("empty response from Anthropic")
}

// callOllama calls the Ollama API
func (cb *ChatBot) callOllama(ctx context.Context, messages []session.Message) (string, error) {
	ctx, span := cb.tracer.Start(ctx, "ollama_api_call")
	defer span.End()

	start := time.Now()

	reqMessages := make([]map[string]string, len(messages))
	for i, msg := range messages {
		reqMessages[i] = map[string]string{
			"role":    msg.Role,
			"content": msg.Content,
		}
	}

	reqBody := backend.OllamaRequest{
		Model:    cb.config.OllamaModel,
		Messages: reqMessages,
		Stream:   false,
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", "http://localhost:11434/api/chat", bytes.NewBuffer(jsonData))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("content-type", "application/json")

	resp, err := cb.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("API error: %s - %s", resp.Status, string(body))
	}

	var apiResp backend.OllamaResponse
	if err := json.Unmarshal(body, &apiResp); err != nil {
		return "", fmt.Errorf("failed to unmarshal response: %w", err)
	}

	duration := time.Since(start)
	histogram, err := cb.meter.Float64Histogram(
		"http.client.request.duration",
		metric.WithDescription("HTTP request duration in milliseconds"),
	)
	if err == nil {
		histogram.Record(ctx, float64(duration.Milliseconds()))
	}

	return apiResp.Message.Content, nil
}

// callGrok calls the Grok API
func (cb *ChatBot) callGrok(ctx context.Context, messages []session.Message) (string, error) {
	ctx, span := cb.tracer.Start(ctx, "grok_api_call")
	defer span.End()

	start := time.Now()

	apiKey := os.Getenv("GROK_API_KEY")
	if apiKey == "" {
		return "", fmt.Errorf("GROK_API_KEY not set")
	}

	reqMessages := make([]map[string]string, len(messages))
	for i, msg := range messages {
		reqMessages[i] = map[string]string{
			"role":    msg.Role,
			"content": msg.Content,
		}
	}

	reqBody := backend.OpenAIRequest{
		Model:    "grok-1",
		Messages: reqMessages,
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", "https://api.grok.x.ai/v1/chat/completions", bytes.NewBuffer(jsonData))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("content-type", "application/json")

	resp, err := cb.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("API error: %s - %s", resp.Status, string(body))
	}

	var apiResp backend.OpenAIResponse
	if err := json.Unmarshal(body, &apiResp); err != nil {
		return "", fmt.Errorf("failed to unmarshal response: %w", err)
	}

	duration := time.Since(start)
	histogram, err := cb.meter.Float64Histogram(
		"http.client.request.duration",
		metric.WithDescription("HTTP request duration in milliseconds"),
	)
	if err == nil {
		histogram.Record(ctx, float64(duration.Milliseconds()))
	}

	cb.recordMetrics(ctx, apiResp.Usage)

	if len(apiResp.Choices) > 0 {
		return apiResp.Choices[0].Message.Content, nil
	}

	return "", fmt.Errorf("empty response from Grok")
}

// callOpenAI calls the OpenAI API
func (cb *ChatBot) callOpenAI(ctx context.Context, messages []session.Message) (string, error) {
	ctx, span := cb.tracer.Start(ctx, "openai_api_call")
	defer span.End()

	start := time.Now()

	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		return "", fmt.Errorf("OPENAI_API_KEY not set")
	}

	reqMessages := make([]map[string]string, len(messages))
	for i, msg := range messages {
		reqMessages[i] = map[string]string{
			"role":    msg.Role,
			"content": msg.Content,
		}
	}

	reqBody := backend.OpenAIRequest{
		Model:    "gpt-3.5-turbo",
		Messages: reqMessages,
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", "https://api.openai.com/v1/chat/completions", bytes.NewBuffer(jsonData))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("content-type", "application/json")

	resp, err := cb.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("API error: %s - %s", resp.Status, string(body))
	}

	var apiResp backend.OpenAIResponse
	if err := json.Unmarshal(body, &apiResp); err != nil {
		return "", fmt.Errorf("failed to unmarshal response: %w", err)
	}

	duration := time.Since(start)
	histogram, err := cb.meter.Float64Histogram(
		"http.client.request.duration",
		metric.WithDescription("HTTP request duration in milliseconds"),
	)
	if err == nil {
		histogram.Record(ctx, float64(duration.Milliseconds()))
	}

	cb.recordMetrics(ctx, apiResp.Usage)

	if len(apiResp.Choices) > 0 {
		return apiResp.Choices[0].Message.Content, nil
	}

	return "", fmt.Errorf("empty response from OpenAI")
}

// listOllamaModels fetches the list of available Ollama models
func (cb *ChatBot) listOllamaModels(ctx context.Context) ([]backend.OllamaModel, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", "http://localhost:11434/api/tags", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := cb.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request (is Ollama running?): %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API error: %s - %s", resp.Status, string(body))
	}

	var tagsResp backend.OllamaTagsResponse
	if err := json.Unmarshal(body, &tagsResp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	return tagsResp.Models, nil
}

// sendMessage sends a message to the current backend
func (cb *ChatBot) sendMessage(ctx context.Context, userMessage string) (string, error) {
	cb.mu.Lock()
	cb.session.Messages = append(cb.session.Messages, session.Message{
		Role:      "user",
		Content:   userMessage,
		Timestamp: time.Now(),
	})
	messages := make([]session.Message, len(cb.session.Messages))
	copy(messages, cb.session.Messages)
	backend := cb.session.Backend
	cb.mu.Unlock()

	cacheKey := cache.GenerateCacheKey(messages)
	if cached, ok := cb.checkCache(cacheKey); ok {
		cb.mu.Lock()
		cb.session.Messages = append(cb.session.Messages, session.Message{
			Role:      "assistant",
			Content:   cached,
			Timestamp: time.Now(),
		})
		cb.mu.Unlock()
		return cached, nil
	}

	var response string
	var err error

	switch backend {
	case config.BackendOllama:
		response, err = cb.callOllama(ctx, messages)
	case config.BackendAnthropic:
		response, err = cb.callAnthropic(ctx, messages)
	case config.BackendGrok:
		response, err = cb.callGrok(ctx, messages)
	case config.BackendOpenAI:
		response, err = cb.callOpenAI(ctx, messages)
	default:
		return "", fmt.Errorf("unknown backend: %s", backend)
	}

	if err != nil {
		return "", err
	}

	cb.storeCache(cacheKey, response)

	cb.mu.Lock()
	cb.session.Messages = append(cb.session.Messages, session.Message{
		Role:      "assistant",
		Content:   response,
		Timestamp: time.Now(),
	})
	cb.mu.Unlock()

	go func() {
		if err := cb.saveSession(); err != nil {
			cb.logger.Error("failed to save session", "error", err)
		}
	}()

	return response, nil
}

// handleCommand handles special commands
func (cb *ChatBot) handleCommand(cmd string) (bool, error) {
	parts := strings.Fields(cmd)
	if len(parts) == 0 {
		return false, nil
	}

	switch parts[0] {
	case "/quit", "/exit":
		return true, nil

	case "/new-session":
		if err := cb.saveSession(); err != nil {
			cb.logger.Error("failed to save current session", "error", err)
		}
		cb.session = cb.newSession()
		fmt.Println("Started new session:", cb.session.ID)
		return false, nil

	case "/switch":
		if len(parts) < 2 {
			return false, fmt.Errorf("usage: /switch <backend> (ollama|anthropic|grok|openai)")
		}
		backendName := parts[1]
		switch backendName {
		case config.BackendOllama, config.BackendAnthropic, config.BackendGrok, config.BackendOpenAI:
			cb.mu.Lock()
			cb.session.Backend = backendName
			cb.mu.Unlock()
			fmt.Printf("Switched to %s backend\n", backendName)
		default:
			return false, fmt.Errorf("unknown backend: %s", backendName)
		}
		return false, nil

	case "/list-ollama-models":
		ctx := context.Background()
		models, err := cb.listOllamaModels(ctx)
		if err != nil {
			return false, fmt.Errorf("failed to list Ollama models: %w", err)
		}
		fmt.Println("\nAvailable Ollama models:")
		for i, model := range models {
			sizeGB := float64(model.Size) / (1024 * 1024 * 1024)
			current := ""
			if model.Name == cb.config.OllamaModel {
				current = " (current)"
			}
			fmt.Printf("%d. %s - %.2f GB%s\n", i+1, model.Name, sizeGB, current)
		}
		fmt.Println()
		return false, nil

	case "/set-ollama-model":
		if len(parts) < 2 {
			return false, fmt.Errorf("usage: /set-ollama-model <model:version>")
		}
		modelName := parts[1]
		cb.mu.Lock()
		cb.config.OllamaModel = modelName
		cb.mu.Unlock()
		fmt.Printf("Ollama model set to: %s\n", modelName)
		return false, nil

	case "/mcp-list":
		if !cb.config.MCPEnabled || cb.mcpRegistry == nil {
			fmt.Println("MCP is not enabled. Use --mcp-enabled flag to enable.")
			return false, nil
		}
		if len(cb.mcpTools) == 0 {
			fmt.Println("No MCP tools available.")
			return false, nil
		}
		fmt.Println("\nAvailable MCP Tools:")
		for i, tool := range cb.mcpTools {
			fmt.Printf("%d. %s (%s)\n", i+1, tool.Name, tool.ServerName)
			fmt.Printf("   %s\n", tool.Description)
		}
		fmt.Println()
		return false, nil

	case "/mcp-servers":
		if !cb.config.MCPEnabled || cb.mcpRegistry == nil {
			fmt.Println("MCP is not enabled. Use --mcp-enabled flag to enable.")
			return false, nil
		}
		clients := cb.mcpRegistry.All()
		if len(clients) == 0 {
			fmt.Println("No MCP servers connected.")
			return false, nil
		}
		fmt.Println("\nConnected MCP Servers:")
		for i, client := range clients {
			fmt.Printf("%d. %s\n", i+1, client.Name())
		}
		fmt.Printf("\nTotal: %d servers, %d tools\n\n", len(clients), len(cb.mcpTools))
		return false, nil

	case "/mcp-reload":
		if !cb.config.MCPEnabled || cb.mcpRegistry == nil {
			fmt.Println("MCP is not enabled. Use --mcp-enabled flag to enable.")
			return false, nil
		}
		ctx := context.Background()
		if err := cb.refreshMCPTools(ctx); err != nil {
			return false, fmt.Errorf("failed to reload MCP tools: %w", err)
		}
		fmt.Printf("Reloaded MCP tools. Total: %d tools from %d servers\n", len(cb.mcpTools), cb.mcpRegistry.Count())
		return false, nil

	case "/help":
		fmt.Println("Available commands:")
		fmt.Println("  /quit, /exit              - Exit the chatbot")
		fmt.Println("  /new-session              - Start a new chat session")
		fmt.Println("  /switch <backend>         - Switch LLM backend (ollama|anthropic|grok|openai)")
		fmt.Println("  /list-ollama-models       - List available Ollama models")
		fmt.Println("  /set-ollama-model <model> - Set Ollama model (e.g., llama3:latest)")
		if cb.config.MCPEnabled {
			fmt.Println("  /mcp-list                 - List all available MCP tools")
			fmt.Println("  /mcp-servers              - Show connected MCP servers")
			fmt.Println("  /mcp-reload               - Reload tools from MCP servers")
		}
		fmt.Println("  /help                     - Show this help message")
		return false, nil

	default:
		return false, nil
	}
}

// Run starts the chat bot
func (cb *ChatBot) Run() error {
	defer cb.db.Close()

	fmt.Println("=== Go Chatbot ===")
	fmt.Printf("Session: %s\n", cb.session.ID)
	fmt.Printf("Backend: %s\n", cb.session.Backend)
	fmt.Println("Type /help for commands, /quit to exit")
	fmt.Println()

	scanner := bufio.NewScanner(os.Stdin)
	ctx := context.Background()

	for {
		fmt.Print("You: ")
		if !scanner.Scan() {
			break
		}

		input := strings.TrimSpace(scanner.Text())
		if input == "" {
			continue
		}

		if strings.HasPrefix(input, "/") {
			shouldQuit, err := cb.handleCommand(input)
			if err != nil {
				fmt.Printf("Error: %v\n", err)
				cb.logger.Error("command error", "error", err)
			}
			if shouldQuit {
				break
			}
			continue
		}

		response, err := cb.sendMessage(ctx, input)
		if err != nil {
			fmt.Printf("Error: %v\n", err)
			cb.logger.Error("failed to send message", "error", err)
			continue
		}

		fmt.Printf("Bot: %s\n\n", response)
	}

	if err := cb.saveSession(); err != nil {
		cb.logger.Error("failed to save session on exit", "error", err)
		return err
	}

	fmt.Println("Goodbye!")
	return nil
}

// handleAnthropicToolUse handles tool use responses from Anthropic
func (cb *ChatBot) handleAnthropicToolUse(ctx context.Context, messages []session.Message, apiResp backend.AnthropicResponse) (string, error) {
	cb.logger.Info("handling tool use", "tools_count", len(apiResp.Content))

	// Extract tool use requests and invoke them
	toolResults := []backend.AnthropicContent{}
	var assistantContent []backend.AnthropicContent

	// First, collect the assistant's response (which includes tool_use blocks)
	assistantContent = apiResp.Content

	// Process each content block
	for _, content := range apiResp.Content {
		if content.Type == "tool_use" {
			cb.logger.Info("invoking MCP tool", "tool", content.Name, "id", content.ID)

			// Call the MCP tool
			result, err := cb.invokeMCPTool(ctx, content.Name, content.Input)

			var toolResult backend.AnthropicContent
			if err != nil {
				// Tool invocation failed
				cb.logger.Error("tool invocation failed", "tool", content.Name, "error", err)
				toolResult = backend.AnthropicContent{
					Type:      "tool_result",
					ToolUseID: content.ID,
					Content:   fmt.Sprintf("Error: %v", err),
					IsError:   true,
				}
			} else {
				// Tool invocation succeeded
				// Convert result to string for simplicity
				resultStr, err := json.Marshal(result)
				if err != nil {
					resultStr = []byte(fmt.Sprintf("%v", result))
				}
				toolResult = backend.AnthropicContent{
					Type:      "tool_result",
					ToolUseID: content.ID,
					Content:   string(resultStr),
				}
			}
			toolResults = append(toolResults, toolResult)
		}
	}

	if len(toolResults) == 0 {
		return "", fmt.Errorf("tool_use stop reason but no tool_use blocks found")
	}

	// Build a new request with the assistant's response and tool results
	// Convert existing messages to Anthropic format
	reqMessages := make([]backend.AnthropicMessage, len(messages))
	for i, msg := range messages {
		reqMessages[i] = backend.AnthropicMessage{
			Role:    msg.Role,
			Content: msg.Content,
		}
	}

	// Add the assistant's message with tool_use blocks
	reqMessages = append(reqMessages, backend.AnthropicMessage{
		Role:    "assistant",
		Content: assistantContent,
	})

	// Add the user's message with tool results
	reqMessages = append(reqMessages, backend.AnthropicMessage{
		Role:    "user",
		Content: toolResults,
	})

	// Make another API call with tool results
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		return "", fmt.Errorf("ANTHROPIC_API_KEY not set")
	}

	reqBody := backend.AnthropicRequest{
		Model:     "claude-sonnet-4-20250514",
		MaxTokens: 1024,
		Messages:  reqMessages,
		Tools:     cb.convertMCPToolsToAnthropic(),
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("failed to marshal follow-up request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", "https://api.anthropic.com/v1/messages", bytes.NewBuffer(jsonData))
	if err != nil {
		return "", fmt.Errorf("failed to create follow-up request: %w", err)
	}

	req.Header.Set("x-api-key", apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")
	req.Header.Set("content-type", "application/json")

	resp, err := cb.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to send follow-up request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read follow-up response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("API error on follow-up: %s - %s", resp.Status, string(body))
	}

	var followUpResp backend.AnthropicResponse
	if err := json.Unmarshal(body, &followUpResp); err != nil {
		return "", fmt.Errorf("failed to unmarshal follow-up response: %w", err)
	}

	cb.recordMetrics(ctx, followUpResp.Usage)

	// Check if we need to handle more tool use (recursive)
	if followUpResp.StopReason == "tool_use" {
		// Recursive tool use - update messages and call again
		// Add assistant's tool use message to our history
		messages = append(messages, session.Message{
			Role:      "assistant",
			Content:   "[Tool use in progress]",
			Timestamp: time.Now(),
		})
		// Add tool results to history
		messages = append(messages, session.Message{
			Role:      "user",
			Content:   "[Tool results]",
			Timestamp: time.Now(),
		})
		return cb.handleAnthropicToolUse(ctx, messages, followUpResp)
	}

	// Extract final text response
	for _, content := range followUpResp.Content {
		if content.Type == "text" {
			return content.Text, nil
		}
	}

	return "", fmt.Errorf("empty response after tool use")
}

// initializeMCP sets up MCP clients based on config
func (cb *ChatBot) initializeMCP() error {
	ctx := context.Background()
	cb.mcpRegistry = mcp.NewClientRegistry()

	// Initialize local Python MCP servers
	for _, scriptPath := range cb.config.MCPLocalServers {
		client, err := mcp.NewStdioClient(scriptPath, scriptPath, cb.logger)
		if err != nil {
			cb.logger.Warn("failed to create stdio MCP client", "script", scriptPath, "error", err)
			continue
		}

		if err := client.Initialize(ctx); err != nil {
			cb.logger.Warn("failed to initialize stdio MCP client", "script", scriptPath, "error", err)
			client.Close()
			continue
		}

		cb.mcpRegistry.Register(scriptPath, client)
		cb.logger.Info("registered local MCP server", "script", scriptPath)
	}

	// Initialize remote MCP servers
	for _, serverURL := range cb.config.MCPRemoteServers {
		var client mcp.MCPClient
		var err error

		// Determine protocol based on URL prefix
		if strings.HasPrefix(serverURL, "ws://") || strings.HasPrefix(serverURL, "wss://") {
			client, err = mcp.NewWebSocketClient(serverURL, serverURL, cb.logger)
		} else {
			client, err = mcp.NewHTTPClient(serverURL, serverURL, cb.logger)
		}

		if err != nil {
			cb.logger.Warn("failed to create remote MCP client", "url", serverURL, "error", err)
			continue
		}

		if err := client.Initialize(ctx); err != nil {
			cb.logger.Warn("failed to initialize remote MCP client", "url", serverURL, "error", err)
			client.Close()
			continue
		}

		cb.mcpRegistry.Register(serverURL, client)
		cb.logger.Info("registered remote MCP server", "url", serverURL)
	}

	// Refresh tools from all MCP servers
	if err := cb.refreshMCPTools(ctx); err != nil {
		return fmt.Errorf("failed to refresh MCP tools: %w", err)
	}

	cb.logger.Info("MCP initialized", "servers", cb.mcpRegistry.Count(), "tools", len(cb.mcpTools))
	return nil
}

// refreshMCPTools fetches all available tools from MCP servers
func (cb *ChatBot) refreshMCPTools(ctx context.Context) error {
	cb.mcpTools = []mcp.Tool{}

	for _, client := range cb.mcpRegistry.All() {
		tools, err := client.ListTools(ctx)
		if err != nil {
			cb.logger.Warn("failed to list tools from MCP server", "server", client.Name(), "error", err)
			continue
		}

		cb.mcpTools = append(cb.mcpTools, tools...)
		cb.logger.Info("loaded tools from MCP server", "server", client.Name(), "count", len(tools))
	}

	return nil
}

// invokeMCPTool calls an MCP tool and returns the result
func (cb *ChatBot) invokeMCPTool(ctx context.Context, toolName string, args map[string]interface{}) (interface{}, error) {
	// Find which server provides this tool
	var targetClient mcp.MCPClient
	for _, tool := range cb.mcpTools {
		if tool.Name == toolName {
			client, ok := cb.mcpRegistry.Get(tool.ServerName)
			if !ok {
				return nil, fmt.Errorf("server %s not found for tool %s", tool.ServerName, toolName)
			}
			targetClient = client
			break
		}
	}

	if targetClient == nil {
		return nil, fmt.Errorf("tool %s not found", toolName)
	}

	// Call the tool
	result, err := targetClient.CallTool(ctx, toolName, args)
	if err != nil {
		return nil, fmt.Errorf("failed to call tool %s: %w", toolName, err)
	}

	cb.logger.Info("invoked MCP tool", "tool", toolName, "server", targetClient.Name())
	return result, nil
}
