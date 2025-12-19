# Go Chatbot - Technical Specification

## Overview

You are an expert Go developer tasked with building a command-line chat bot application in Go. The program must be multi-threaded, portable across Windows, Linux, and macOS, and adhere strictly to the following requirements. Use Go 1.23 or later for modern features like generics and error handling. Ensure the code is efficient, idiomatic, and well-commented. Handle errors gracefully with logging.

**Principle**: Do not use external dependencies unless explicitly mentioned; prefer standard library where possible.

---

## Core Requirements

### Functionality
- Command-line application that interacts via stdin/stdout
- Multi-turn conversation support (chat sessions)
- Multi-threaded architecture using goroutines for:
  - Concurrent API calls
  - Background logging
  - Metrics collection
  - Multiple session handling
- Backend switching via command-line flags or in-chat commands
- Request/response caching (in-memory and persistent)

### Portability
- Cross-platform: Windows, Linux, macOS
- No OS-specific code
- Use `filepath.Join` for all path operations
- Lightweight with minimal allocations

---

## LLM Backends

The chatbot supports multiple LLM backends that can be switched dynamically:

### 1. Ollama (Default)
- **Endpoint**: `http://localhost:11434/api/chat`
- **Configuration**: Specify model using format `[llm-name:version]`
  - Examples: `llama3:latest`, `llama3:2023-06-01`
- **Features**:
  - Ability to list all available Ollama models
  - Model selection from available models
- **Assumption**: Ollama running locally

### 2. Anthropic Claude
- **Endpoint**: `https://api.anthropic.com/v1/messages`
- **Headers**:
  ```
  x-api-key: $ANTHROPIC_API_KEY
  anthropic-version: 2023-06-01
  content-type: application/json
  ```
- **Request Format**:
  ```json
  {
    "model": "claude-sonnet-4-20250514",
    "max_tokens": 1024,
    "messages": [{"role": "user", "content": "Hello, world"}]
  }
  ```
- **Response**: Extract `content` for bot reply and `usage` for metrics
- **Special Handling**: Parse cache-related fields (e.g., `cache_creation.ephemeral_5m_input_tokens`)

### 3. Grok (xAI)
- **Endpoint**: `https://api.x.ai/v1/chat/completions`
- **Headers**:
  ```
  Content-Type: application/json
  Authorization: Bearer $GROK_API_KEY
  ```
- **Request Format**: OpenAI-compatible
  ```json
  {
    "model": "grok-4-latest",
    "messages": [
      {"role": "system", "content": "You are a test assistant."},
      {"role": "user", "content": "Testing."}
    ],
    "stream": false,
    "temperature": 0
  }
  ```

### 4. OpenAI
- **Endpoint**: `https://api.openai.com/v1/chat/completions`
- **Format**: Standard OpenAI chat completions API
- **Headers**: `Authorization: Bearer $OPENAI_API_KEY`

### Backend Configuration
- API keys loaded from environment variables:
  - `ANTHROPIC_API_KEY`
  - `OPENAI_API_KEY`
  - `GROK_API_KEY`
- Switch backends using:
  - Command-line flag: `--backend [ollama|anthropic|grok|openai]`
  - In-chat commands: `/switch ollama`, `/switch anthropic`, `/switch grok`, `/switch openai`

---

## Model Context Protocol (MCP) Client

### Overview
Implement an MCP client that handles both LOCAL and REMOTE MCP servers for extending chatbot capabilities.

### Server Types

#### Local MCP Servers
- Support for local MCP servers (e.g., Python-based)
- STDIO-based communication
- Process management for local server lifecycle

#### Remote MCP Servers
- HTTP/WebSocket connections to remote servers
- Format: `http://{HOST_NAME}:{PORT}` or `ws://{HOST_NAME}:{PORT}`
- Connection pooling and retry logic

### Features
- **Service Discovery**: List all configured MCP services
- **Service Details**: Provide detailed information about each MCP connection (status, capabilities, endpoints)
- **Built-in Services**: Include "Search the Web" in the MCP service list
- **Multi-protocol Support**: STDIO, HTTP, and WebSocket transports

### Integration
- MCP calls should be traceable via OpenTelemetry
- Responses cached similar to LLM responses
- Error handling with fallback mechanisms

---

## Data Persistence

### In-Memory Cache
- Use `sync.Map` for thread-safe caching
- Cache key: Hash of message array
- Check cache before making API calls
- Store responses on cache miss

### SQLite Database
- **Library**: `github.com/mattn/go-sqlite3`
- **Tables**:
  - `sessions`: id, start_time, backend, created_at
  - `messages`: session_id, role, content, timestamp
- **Persistence**: Save sessions on exit or periodically
- **Session Management**: Unique ID per session with timestamp tracking

---

## Logging

### Configuration
- **DO NOT** output logs to stdout
- **Log File**: `./logs/chatbot.log`
- **Rotation**: Every 10MB using `github.com/natefinch/lumberjack`
- **Format**: Structured logging with `log/slog` (Go stdlib)
- **Collection**: OTEL collector picks up logs automatically when running locally

### Log Files
- `./logs/chatbot.log` - Main application logs
- `./logs/chatbot_traces.log` - Trace output for debugging
- `./logs/metrics_traces.log` - Metrics output for debugging

---

## Observability (OpenTelemetry)

### Tracing
- **Library**: `go.opentelemetry.io/otel`
- **DO NOT** send traces to stdout
- Trace all LLM API calls with spans
- Span details:
  - Full request/response cycle
  - Backend used
  - Model selected
  - Error status
- **Debug Output**: Write traces to `./logs/chatbot_traces.log`
- **Collection**: Assume OTEL collector running locally picks up traces automatically

### Metrics
- **DO NOT** send metrics to stdout
- **Response Time**: Histogram for latency in milliseconds
- **Usage Metrics**: From response JSON `usage` field
  - Extract all integer values (input_tokens, output_tokens, etc.)
  - Create gauges or counters for each metric
  - Handle provider-specific fields (e.g., Anthropic cache fields)
- **Naming**: Use OTEL semantic conventions (e.g., `http.client.request.duration`)
- **Debug Output**: Write metrics to `./logs/metrics_traces.log`
- **Collection**: Assume OTEL collector running locally picks up metrics automatically

### OTEL Collector Configuration
- Application configured to work with local OTEL collector
- No stdout exporters (console exporters removed)
- Collector handles all telemetry routing and export
- See README.md for collector setup details

---

## Command-Line Interface

### Execution
```bash
./chatbot [flags]
```

### Flags
- `--backend [ollama|anthropic|grok|openai]` - Select LLM backend (default: ollama)
- `--session-id <id>` - Load existing session
- `--debug` - Enable verbose logging

### In-Chat Commands
- `/quit` - Exit the chatbot
- `/new-session` - Start a new chat session
- `/switch <backend>` - Switch to different LLM backend
  - Examples: `/switch ollama`, `/switch anthropic`, `/switch grok`, `/switch openai`

### User Interaction
- User types messages via stdin
- Bot responds via stdout
- Clean console output (no logs or telemetry in stdout)
- Multi-turn conversation within sessions

---

## Dependencies

### Required External Libraries
- `github.com/mattn/go-sqlite3` - SQLite database driver
- `github.com/natefinch/lumberjack` - Log rotation
- `go.opentelemetry.io/otel` - OpenTelemetry core
- `go.opentelemetry.io/otel/trace` - Tracing support
- `go.opentelemetry.io/otel/metric` - Metrics support
- `go.opentelemetry.io/otel/sdk` - OTEL SDK components

### Standard Library
Prefer standard library where possible:
- `net/http` - HTTP client
- `encoding/json` - JSON handling
- `os` - Environment and file operations
- `fmt` - Formatting
- `time` - Timestamps
- `sync` - Concurrency primitives
- `log/slog` - Structured logging

---

## Build and Deployment

### Code Organization
- Use standard Go project layout:
  - `cmd/chatbot/` - Main application
  - `internal/` - Internal packages
  - `pkg/` - Public packages (if needed)

### Build Instructions
```bash
go build -o chatbot ./cmd/chatbot
```

### Environment Variables
Required before running:
```bash
export ANTHROPIC_API_KEY="your-key-here"
export OPENAI_API_KEY="your-key-here"
export GROK_API_KEY="your-key-here"
```

### Documentation
- Include README.md with:
  - Build instructions
  - Environment variable setup
  - Usage examples
  - OTEL collector configuration
  - Tracing and metrics documentation
  - MCP server configuration

### Code Quality
- Efficient and idiomatic Go code
- Well-commented for clarity
- Graceful error handling
- Comprehensive logging
- Thread-safe operations