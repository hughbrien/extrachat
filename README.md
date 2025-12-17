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
- `/list-ollama-models` - List all available Ollama models
  - Shows model names with sizes and indicates the current model
- `/set-ollama-model <model>` - Change the Ollama model
  - Example: `/set-ollama-model codellama:13b`
  - Example: `/set-ollama-model mistral:7b`
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
├── main.go                        # Main application code
├── go.mod                         # Go module definition
├── go.sum                         # Dependency checksums (generated)
├── README.md                      # This file
├── program_prompt.md              # Original requirements specification
├── chatbot.db                     # SQLite database (generated)
├── otel-config/
│   └── otel-config.yaml           # OTEL collector configuration
└── logs/
    ├── chatbot.log                # Application logs (generated)
    ├── extrachat_traces_process.log   # OpenTelemetry traces (generated)
    ├── extrachat_metrics_process.log  # OpenTelemetry metrics (generated)
    ├── extrachat.logs             # OTEL collector logs (generated)
    ├── extrachat_metrics.logs     # OTEL collector metrics (generated)
    └── extrachat_traces.logs      # OTEL collector traces (generated)
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
- **Application Logs**: `./logs/chatbot.log` with automatic rotation (JSON format)
- **Trace Logs**: `./logs/extrachat_traces_process.log` with automatic rotation (JSON format)
- **Metrics Logs**: `./logs/extrachat_metrics_process.log` with automatic rotation (JSON format, exported every 10 seconds)

Log files rotate when they reach 10MB in size, keeping up to 3 backups. Old logs are compressed.

Note: Logs are NOT written to stdout to keep the console clean for chat interactions. The OTEL collector running locally will automatically pick up log data and export to its configured destinations.

## OpenTelemetry

The application is fully instrumented with OpenTelemetry for tracing and metrics:

### Traces

Full request/response cycles for each LLM call are automatically traced:

**Spans Created:**
- `anthropic_api_call` - Anthropic Claude API requests
- `ollama_api_call` - Ollama local model requests
- `grok_api_call` - xAI Grok API requests
- `openai_api_call` - OpenAI API requests

Each span includes:
- Request duration and timing
- HTTP status codes
- Error tracking
- Service name and version attributes

**Trace Output:**
- Automatically written to `./logs/extrachat_traces_process.log` in JSON format
- File uses automatic rotation (10MB limit, 3 backups, compressed)
- NOT written to stdout to keep console clean
- Available for OTEL collector to pick up via SDK

**Viewing Traces:**
```bash
# View latest traces
tail -f ./logs/extrachat_traces_process.log

# Pretty print JSON traces
cat ./logs/extrachat_traces_process.log | jq '.'

# Search for specific spans
grep "anthropic_api_call" ./logs/extrachat_traces_process.log | jq '.'

# Filter by errors only
cat ./logs/extrachat_traces_process.log | jq 'select(.Status.Code == "Error")'
```

### Metrics

The chatbot automatically collects performance and usage metrics:

**HTTP Request Metrics:**
- `http.client.request.duration` - HTTP request duration histogram (milliseconds)
  - Labels: backend, status code
  - Tracks latency for all LLM API calls

**LLM Usage Metrics:**
- `llm.usage.input_tokens` - Input tokens processed
- `llm.usage.output_tokens` - Tokens generated
- `llm.usage.cache_creation_input_tokens` - Tokens used for cache creation
- `llm.usage.cache_read_input_tokens` - Tokens read from cache
- `llm.usage.cache_creation.ephemeral_5m_input_tokens` - Ephemeral cache (5-min TTL)
- `llm.usage.cache_creation.ephemeral_1h_input_tokens` - Ephemeral cache (1-hour TTL)
- `llm.usage.prompt_tokens` - Tokens in prompt (OpenAI/Grok)
- `llm.usage.completion_tokens` - Tokens in completion (OpenAI/Grok)
- `llm.usage.total_tokens` - Total tokens used (OpenAI/Grok)

**Metrics Output:**
- Automatically written to `./logs/extrachat_metrics_process.log` in JSON format
- Exported every 10 seconds
- File uses automatic rotation (10MB limit, 3 backups, compressed)
- NOT written to stdout to keep console clean
- Available for OTEL collector to pick up via SDK

**Viewing Metrics:**
```bash
# View latest metrics
tail -f ./logs/extrachat_metrics_process.log

# Pretty print JSON metrics
cat ./logs/extrachat_metrics_process.log | jq '.'

# Extract specific metric
cat ./logs/extrachat_metrics_process.log | jq '.ScopeMetrics[].Metrics[] | select(.Name == "http.client.request.duration")'

# Get all LLM usage metrics
cat ./logs/extrachat_metrics_process.log | jq '.ScopeMetrics[].Metrics[] | select(.Name | startswith("llm.usage"))'

# Calculate average request duration
cat ./logs/extrachat_metrics_process.log | jq '.ScopeMetrics[].Metrics[] | select(.Name == "http.client.request.duration") | .Data.DataPoints[0] | .Sum / .Count'
```

### OTEL Collector Setup

The application works with an OpenTelemetry Collector for advanced observability.

**Installing via Docker (Recommended):**
```bash
# Pull and run the OTEL Collector
docker pull otel/opentelemetry-collector:latest

docker run -d \
  --name otel-collector \
  -p 4317:4317 \
  -p 4318:4318 \
  -p 55679:55679 \
  -v $(pwd)/otel-config/otel-config.yaml:/etc/otel-collector-config.yaml \
  otel/opentelemetry-collector:latest \
  --config=/etc/otel-collector-config.yaml
```

**Configuration File:** See `otel-config/otel-config.yaml` for the collector configuration.

**Environment Variables (Optional):**
```bash
# OTLP gRPC endpoint (default: localhost:4317)
export OTEL_EXPORTER_OTLP_ENDPOINT=http://localhost:4317

# OTLP HTTP endpoint (alternative)
export OTEL_EXPORTER_OTLP_ENDPOINT=http://localhost:4318

# Service name (override default)
export OTEL_SERVICE_NAME=chatbot

# Resource attributes
export OTEL_RESOURCE_ATTRIBUTES=deployment.environment=production,service.version=1.0.0
```

### Integration with Observability Tools

**Jaeger (Distributed Tracing):**
```bash
# Run Jaeger
docker run -d --name jaeger \
  -p 16686:16686 \
  -p 14250:14250 \
  jaegertracing/all-in-one:latest

# Access UI at http://localhost:16686
```

**Prometheus & Grafana (Metrics):**
```bash
# Run Prometheus
docker run -d --name prometheus \
  -p 9090:9090 \
  -v $(pwd)/prometheus.yml:/etc/prometheus/prometheus.yml \
  prom/prometheus

# Run Grafana
docker run -d --name grafana \
  -p 3000:3000 \
  grafana/grafana

# Access Grafana at http://localhost:3000
```

**Prometheus Configuration (`prometheus.yml`):**
```yaml
scrape_configs:
  - job_name: 'chatbot'
    scrape_interval: 10s
    static_configs:
      - targets: ['localhost:8889']
```

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
