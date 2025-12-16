# Go Chatbot

A multi-threaded, portable command-line chatbot application written in Go that supports multiple LLM backends with caching, logging, and OpenTelemetry observability.

## Features

- **Multiple LLM Backends**: Support for Ollama, Anthropic, Grok (xAI), and OpenAI
- **Multi-threaded**: Concurrent API calls, logging, and metrics collection using goroutines
- **Caching**: In-memory cache with SQLite persistence for request/response storage
- **Structured Logging**: JSON logging to file with automatic rotation (10MB)
- **OpenTelemetry**: Full tracing and metrics for all LLM API calls
- **Session Management**: Persistent chat sessions with SQLite database
- **Cross-platform**: Works on Windows, Linux, and macOS

## Prerequisites

- Go 1.23 or later
- SQLite (included via go-sqlite3)
- API keys for the backends you want to use:
  - `ANTHROPIC_API_KEY` for Anthropic Claude
  - `OPENAI_API_KEY` for OpenAI
  - `GROK_API_KEY` for xAI Grok
  - Ollama running locally on port 11434 (no API key needed)

## Installation

1. Clone or download the project:
```bash
cd /path/to/ExtraChat
```

2. Download dependencies:
```bash
go mod download
```

3. Build the application:
```bash
go build -o chatbot main.go
```

For specific platforms:
```bash
# Windows
GOOS=windows GOARCH=amd64 go build -o chatbot.exe main.go

# Linux
GOOS=linux GOARCH=amd64 go build -o chatbot main.go

# macOS
GOOS=darwin GOARCH=amd64 go build -o chatbot main.go
```

## Configuration

Set environment variables for the backends you want to use:

```bash
# For Anthropic
export ANTHROPIC_API_KEY="your-anthropic-api-key"

# For OpenAI
export OPENAI_API_KEY="your-openai-api-key"

# For Grok
export GROK_API_KEY="your-grok-api-key"

# Ollama (no API key needed, just ensure it's running)
# Start Ollama with: ollama serve
```

## Usage

### Basic Usage

Start the chatbot with default settings (Ollama backend):
```bash
./chatbot
```

### Command-Line Flags

- `--backend <name>`: Choose LLM backend (default: ollama)
  - Options: `ollama`, `anthropic`, `grok`, `openai`
- `--session-id <id>`: Load an existing session
- `--debug`: Enable debug logging
- `--ollama-model <model:version>`: Specify Ollama model (default: llama3:latest)
  - Format: `model:version` (e.g., `llama3:latest`, `codellama:13b`, `mistral:7b`)

Examples:
```bash
# Start with Anthropic backend
./chatbot --backend anthropic

# Use specific Ollama model
./chatbot --backend ollama --ollama-model codellama:13b

# Load existing session
./chatbot --session-id session_1234567890

# Enable debug mode
./chatbot --debug

# Combine flags
./chatbot --backend openai --debug
```

### In-Chat Commands

While chatting, you can use these commands:

- `/quit` or `/exit` - Exit the chatbot
- `/new-session` - Start a new chat session
- `/switch <backend>` - Switch to a different LLM backend
  - Example: `/switch anthropic`
- `/help` - Show available commands

### Example Session

```
=== Go Chatbot ===
Session: session_1702345678
Backend: ollama
Type /help for commands, /quit to exit

You: Hello, how are you?
Bot: I'm doing well, thank you for asking! How can I help you today?

You: /switch anthropic
Switched to anthropic backend

You: What's the weather like?
Bot: I don't have access to real-time weather data...

You: /quit
Goodbye!
```

## Project Structure

```
ExtraChat/
├── main.go                   # Main application code
├── go.mod                    # Go module definition
├── go.sum                    # Dependency checksums (generated)
├── README.md                 # This file
├── OTEL_CONFIGURATION.md     # OpenTelemetry configuration guide
├── chatbot.db                # SQLite database (generated)
└── logs/
    └── chatbot.log           # Application logs (generated)
```

## Database Schema

The application creates a SQLite database (`chatbot.db`) with the following schema:

### Sessions Table
- `id`: Session identifier
- `start_time`: Session start timestamp
- `backend`: LLM backend used

### Messages Table
- `id`: Auto-increment message ID
- `session_id`: Foreign key to sessions
- `role`: Message role (user/assistant)
- `content`: Message content
- `timestamp`: Message timestamp

## Logging

Logs are written to:
- **File**: `./logs/chatbot.log` with automatic rotation (JSON format)

Log files rotate when they reach 10MB in size, keeping up to 3 backups. Old logs are compressed.

Note: Logs are NOT written to stdout to keep the console clean for chat interactions. The OTEL collector running locally will automatically pick up log data.

## OpenTelemetry

The application is fully instrumented with OpenTelemetry for tracing and metrics:

- **Traces**: Full request/response cycles for each LLM call
  - Spans for each backend: `anthropic_api_call`, `ollama_api_call`, `grok_api_call`, `openai_api_call`
  - Includes request duration, status codes, and error tracking
- **Metrics**:
  - `http.client.request.duration` - HTTP request duration histogram (milliseconds)
  - `llm.usage.input_tokens` - Input tokens consumed
  - `llm.usage.output_tokens` - Output tokens generated
  - `llm.usage.cache_*` - Cache-related token metrics (Anthropic)
  - All usage fields from API responses automatically extracted

**Note**: Traces and metrics are NOT exported to stdout. Instead, they are designed to be collected by an OTEL collector running locally, which will automatically pick up the telemetry data. This keeps the console output clean and focused on the chat interaction.

**📖 For detailed OpenTelemetry configuration, including:**
- Complete list of traces and spans
- All available metrics with descriptions
- OTEL Collector setup and configuration
- Integration with Jaeger, Prometheus, and Grafana
- Troubleshooting guide

**See: [OTEL_CONFIGURATION.md](OTEL_CONFIGURATION.md)**

## Caching

The application implements two-level caching:

1. **In-memory cache**: Fast access using sync.Map for thread-safety
2. **SQLite persistence**: Long-term storage of all conversations

Cache keys are generated using SHA-256 hashes of the message history.

## Multi-threading

The application uses goroutines for:
- Concurrent API calls
- Background session saving
- Logging operations
- Metrics collection

All shared state is protected with appropriate synchronization primitives (sync.Mutex, sync.Map).

## Error Handling

The application handles errors gracefully:
- API failures are logged and displayed to the user
- Database errors don't crash the application
- Missing API keys provide helpful error messages
- Network timeouts are set to 60 seconds

## Development

### Running Tests
```bash
go test ./...
```

### Code Formatting
```bash
go fmt ./...
```

### Linting
```bash
golangci-lint run
```

## Troubleshooting

### "ANTHROPIC_API_KEY not set"
Set the appropriate environment variable for your chosen backend.

### "Failed to connect to Ollama"
Ensure Ollama is running: `ollama serve`

### "Database is locked"
The SQLite database is in use by another process. Close other instances of the chatbot.

### Log files too large
Log rotation should handle this automatically. Check `./log/` directory and manually delete old logs if needed.

## Performance

The application is designed to be lightweight and efficient:
- Minimal memory allocations
- Connection pooling for HTTP clients
- Efficient caching to reduce API calls
- Async operations for I/O-bound tasks

## License

This is a demonstration project. Modify and use as needed.

## Contributing

Feel free to submit issues or pull requests for improvements.

## Acknowledgments

- Built with Go 1.23+
- Uses OpenTelemetry for observability
- SQLite for data persistence
- Lumberjack for log rotation
