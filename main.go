package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/stdout/stdoutmetric"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.24.0"
	"go.opentelemetry.io/otel/trace"
	lumberjack "gopkg.in/natefinch/lumberjack.v2"
)

const (
	BackendOllama    = "ollama"
	BackendAnthropic = "anthropic"
	BackendGrok      = "grok"
	BackendOpenAI    = "openai"
)

// Message represents a single chat message
type Message struct {
	Role      string    `json:"role"`
	Content   string    `json:"content"`
	Timestamp time.Time `json:"timestamp"`
}

// Session represents a chat session
type Session struct {
	ID        string    `json:"id"`
	StartTime time.Time `json:"start_time"`
	Backend   string    `json:"backend"`
	Messages  []Message `json:"messages"`
}

// Config holds application configuration
type Config struct {
	Backend   string
	SessionID string
	Debug     bool
}

// AnthropicRequest represents the request body for Anthropic API
type AnthropicRequest struct {
	Model     string              `json:"model"`
	MaxTokens int                 `json:"max_tokens"`
	Messages  []map[string]string `json:"messages"`
}

// AnthropicResponse represents the response from Anthropic API
type AnthropicResponse struct {
	ID      string `json:"id"`
	Type    string `json:"type"`
	Role    string `json:"role"`
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	Model        string                 `json:"model"`
	StopReason   string                 `json:"stop_reason"`
	StopSequence string                 `json:"stop_sequence"`
	Usage        map[string]interface{} `json:"usage"`
}

// OllamaRequest represents the request body for Ollama API
type OllamaRequest struct {
	Model    string              `json:"model"`
	Messages []map[string]string `json:"messages"`
	Stream   bool                `json:"stream"`
}

// OllamaResponse represents the response from Ollama API
type OllamaResponse struct {
	Model     string `json:"model"`
	CreatedAt string `json:"created_at"`
	Message   struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	} `json:"message"`
	Done bool `json:"done"`
}

// OpenAIRequest represents the request body for OpenAI-compatible APIs
type OpenAIRequest struct {
	Model    string              `json:"model"`
	Messages []map[string]string `json:"messages"`
}

// OpenAIResponse represents the response from OpenAI-compatible APIs
type OpenAIResponse struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	Model   string `json:"model"`
	Choices []struct {
		Index   int `json:"index"`
		Message struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage map[string]interface{} `json:"usage"`
}

// CachedResponse represents a cached API response
type CachedResponse struct {
	Response  string
	Timestamp time.Time
}

// ChatBot represents the main application
type ChatBot struct {
	config     Config
	db         *sql.DB
	cache      sync.Map
	logger     *slog.Logger
	tracer     trace.Tracer
	meter      metric.Meter
	httpClient *http.Client
	session    *Session
	mu         sync.Mutex
}

// initLogger initializes structured logging with rotation
func initLogger() (*slog.Logger, error) {
	logDir := "log"
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create log directory: %w", err)
	}

	logFile := filepath.Join(logDir, "chatbot.log")

	lumberjackLogger := &lumberjack.Logger{
		Filename:   logFile,
		MaxSize:    10, // 10 MB
		MaxBackups: 3,
		MaxAge:     28,
		Compress:   true,
	}

	multiWriter := io.MultiWriter(os.Stdout, lumberjackLogger)

	handler := slog.NewJSONHandler(multiWriter, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})

	logger := slog.New(handler)
	slog.SetDefault(logger)

	return logger, nil
}

// initTelemetry initializes OpenTelemetry tracing and metrics
func initTelemetry(ctx context.Context) (trace.Tracer, metric.Meter, func(), error) {
	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceName("chatbot"),
			semconv.ServiceVersion("1.0.0"),
		),
	)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to create resource: %w", err)
	}

	traceExporter, err := stdouttrace.New(
		stdouttrace.WithPrettyPrint(),
	)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to create trace exporter: %w", err)
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(traceExporter),
		sdktrace.WithResource(res),
	)
	otel.SetTracerProvider(tp)

	metricExporter, err := stdoutmetric.New(
		stdoutmetric.WithPrettyPrint(),
	)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to create metric exporter: %w", err)
	}

	mp := sdkmetric.NewMeterProvider(
		sdkmetric.WithReader(
			sdkmetric.NewPeriodicReader(
				metricExporter,
				sdkmetric.WithInterval(10*time.Second),
			),
		),
		sdkmetric.WithResource(res),
	)
	otel.SetMeterProvider(mp)

	tracer := tp.Tracer("chatbot")
	meter := mp.Meter("chatbot")

	cleanup := func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := tp.Shutdown(ctx); err != nil {
			slog.Error("failed to shutdown tracer provider", "error", err)
		}
		if err := mp.Shutdown(ctx); err != nil {
			slog.Error("failed to shutdown meter provider", "error", err)
		}
	}

	return tracer, meter, cleanup, nil
}

// initDB initializes the SQLite database
func initDB() (*sql.DB, error) {
	db, err := sql.Open("sqlite3", "chatbot.db")
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	createSessionsTable := `
	CREATE TABLE IF NOT EXISTS sessions (
		id TEXT PRIMARY KEY,
		start_time DATETIME,
		backend TEXT
	);`

	createMessagesTable := `
	CREATE TABLE IF NOT EXISTS messages (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		session_id TEXT,
		role TEXT,
		content TEXT,
		timestamp DATETIME,
		FOREIGN KEY(session_id) REFERENCES sessions(id)
	);`

	if _, err := db.Exec(createSessionsTable); err != nil {
		return nil, fmt.Errorf("failed to create sessions table: %w", err)
	}

	if _, err := db.Exec(createMessagesTable); err != nil {
		return nil, fmt.Errorf("failed to create messages table: %w", err)
	}

	return db, nil
}

// NewChatBot creates a new ChatBot instance
func NewChatBot(config Config) (*ChatBot, error) {
	logger, err := initLogger()
	if err != nil {
		return nil, fmt.Errorf("failed to initialize logger: %w", err)
	}

	ctx := context.Background()
	tracer, meter, _, err := initTelemetry(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize telemetry: %w", err)
	}

	db, err := initDB()
	if err != nil {
		return nil, fmt.Errorf("failed to initialize database: %w", err)
	}

	if config.Debug {
		logger.Info("Debug mode enabled")
	}

	cb := &ChatBot{
		config:     config,
		db:         db,
		logger:     logger,
		tracer:     tracer,
		meter:      meter,
		httpClient: &http.Client{Timeout: 60 * time.Second},
	}

	if config.SessionID != "" {
		session, err := cb.loadSession(config.SessionID)
		if err != nil {
			logger.Warn("failed to load session, creating new one", "error", err)
			cb.session = cb.newSession()
		} else {
			cb.session = session
			logger.Info("loaded existing session", "session_id", session.ID)
		}
	} else {
		cb.session = cb.newSession()
	}

	return cb, nil
}

// newSession creates a new session
func (cb *ChatBot) newSession() *Session {
	sessionID := fmt.Sprintf("session_%d", time.Now().Unix())
	session := &Session{
		ID:        sessionID,
		StartTime: time.Now(),
		Backend:   cb.config.Backend,
		Messages:  []Message{},
	}
	cb.logger.Info("created new session", "session_id", sessionID, "backend", cb.config.Backend)
	return session
}

// loadSession loads a session from the database
func (cb *ChatBot) loadSession(sessionID string) (*Session, error) {
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

	messages := []Message{}
	for rows.Next() {
		var msg Message
		if err := rows.Scan(&msg.Role, &msg.Content, &msg.Timestamp); err != nil {
			return nil, fmt.Errorf("failed to scan message: %w", err)
		}
		messages = append(messages, msg)
	}

	return &Session{
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

// generateCacheKey generates a cache key from messages
func generateCacheKey(messages []Message) string {
	h := sha256.New()
	for _, msg := range messages {
		h.Write([]byte(msg.Role))
		h.Write([]byte(msg.Content))
	}
	return fmt.Sprintf("%x", h.Sum(nil))
}

// checkCache checks if a response is cached
func (cb *ChatBot) checkCache(cacheKey string) (string, bool) {
	if val, ok := cb.cache.Load(cacheKey); ok {
		cached := val.(CachedResponse)
		cb.logger.Info("cache hit", "key", cacheKey[:16])
		return cached.Response, true
	}
	return "", false
}

// storeCache stores a response in cache
func (cb *ChatBot) storeCache(cacheKey, response string) {
	cb.cache.Store(cacheKey, CachedResponse{
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

// callAnthropic calls the Anthropic API
func (cb *ChatBot) callAnthropic(ctx context.Context, messages []Message) (string, error) {
	ctx, span := cb.tracer.Start(ctx, "anthropic_api_call")
	defer span.End()

	start := time.Now()

	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		return "", fmt.Errorf("ANTHROPIC_API_KEY not set")
	}

	reqMessages := make([]map[string]string, len(messages))
	for i, msg := range messages {
		reqMessages[i] = map[string]string{
			"role":    msg.Role,
			"content": msg.Content,
		}
	}

	reqBody := AnthropicRequest{
		Model:     "claude-sonnet-4-20250514",
		MaxTokens: 1024,
		Messages:  reqMessages,
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

	var apiResp AnthropicResponse
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

	if len(apiResp.Content) > 0 {
		return apiResp.Content[0].Text, nil
	}

	return "", fmt.Errorf("empty response from Anthropic")
}

// callOllama calls the Ollama API
func (cb *ChatBot) callOllama(ctx context.Context, messages []Message) (string, error) {
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

	reqBody := OllamaRequest{
		Model:    "llama2",
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

	var apiResp OllamaResponse
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
func (cb *ChatBot) callGrok(ctx context.Context, messages []Message) (string, error) {
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

	reqBody := OpenAIRequest{
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

	var apiResp OpenAIResponse
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
func (cb *ChatBot) callOpenAI(ctx context.Context, messages []Message) (string, error) {
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

	reqBody := OpenAIRequest{
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

	var apiResp OpenAIResponse
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

// sendMessage sends a message to the current backend
func (cb *ChatBot) sendMessage(ctx context.Context, userMessage string) (string, error) {
	cb.mu.Lock()
	cb.session.Messages = append(cb.session.Messages, Message{
		Role:      "user",
		Content:   userMessage,
		Timestamp: time.Now(),
	})
	messages := make([]Message, len(cb.session.Messages))
	copy(messages, cb.session.Messages)
	backend := cb.session.Backend
	cb.mu.Unlock()

	cacheKey := generateCacheKey(messages)
	if cached, ok := cb.checkCache(cacheKey); ok {
		cb.mu.Lock()
		cb.session.Messages = append(cb.session.Messages, Message{
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
	case BackendOllama:
		response, err = cb.callOllama(ctx, messages)
	case BackendAnthropic:
		response, err = cb.callAnthropic(ctx, messages)
	case BackendGrok:
		response, err = cb.callGrok(ctx, messages)
	case BackendOpenAI:
		response, err = cb.callOpenAI(ctx, messages)
	default:
		return "", fmt.Errorf("unknown backend: %s", backend)
	}

	if err != nil {
		return "", err
	}

	cb.storeCache(cacheKey, response)

	cb.mu.Lock()
	cb.session.Messages = append(cb.session.Messages, Message{
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
		backend := parts[1]
		switch backend {
		case BackendOllama, BackendAnthropic, BackendGrok, BackendOpenAI:
			cb.mu.Lock()
			cb.session.Backend = backend
			cb.mu.Unlock()
			fmt.Printf("Switched to %s backend\n", backend)
		default:
			return false, fmt.Errorf("unknown backend: %s", backend)
		}
		return false, nil

	case "/help":
		fmt.Println("Available commands:")
		fmt.Println("  /quit, /exit        - Exit the chatbot")
		fmt.Println("  /new-session        - Start a new chat session")
		fmt.Println("  /switch <backend>   - Switch LLM backend (ollama|anthropic|grok|openai)")
		fmt.Println("  /help               - Show this help message")
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

func main() {
	var config Config
	flag.StringVar(&config.Backend, "backend", BackendOllama, "LLM backend (ollama|anthropic|grok|openai)")
	flag.StringVar(&config.SessionID, "session-id", "", "Load existing session by ID")
	flag.BoolVar(&config.Debug, "debug", false, "Enable debug logging")
	flag.Parse()

	bot, err := NewChatBot(config)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to initialize chatbot: %v\n", err)
		os.Exit(1)
	}

	if err := bot.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
