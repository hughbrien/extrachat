# Go Chatbot

You are an expert Go developer tasked with building a command-line chat bot application in Go. 
The program must be multi-threaded, portable across Windows, Linux, and macOS, and adhere strictly to the following requirements. 
Use Go 1.23 or later for modern features like generics and error handling. 
Ensure the code is efficient, idiomatic, and well-commented. 
Handle errors gracefully with logging. 
Do not use external dependencies unless explicitly mentioned below; prefer standard library where possible.

### Core Functionality
- The bot is a command-line application that interacts with the user via stdin/stdout, allowing multi-turn conversations (chat sessions).
- It supports multiple LLM backends:
    - Initial/default configuration: Local Ollama (via HTTP API at http://localhost:11434/api/chat or equivalent; assume Ollama is running locally).
    - Anthropic Developer API (using the provided HTTP interface example).
    - Grok API (use xAI's API endpoint at https://api.grok.x.ai/v1/chat/completions or equivalent; assume standard OpenAI-compatible format).
    - OpenAI standard API (as a fallback or additional option; endpoint https://api.openai.com/v1/chat/completions).
- The user can switch backends via command-line flags or in-chat commands (e.g., "/switch ollama", "/switch anthropic").
- Cache all requests and responses using an in-memory cache (e.g., with sync.Map for thread-safety) and persist to SQLite for long-term storage.
- Persist all requests/responses as sessions in the latest SQLite database (use github.com/mattn/go-sqlite3). Each session has a unique ID, timestamp, backend used, and a list of message exchanges.
- Make the application multi-threaded: Use goroutines for concurrent API calls, logging, and metrics collection to handle multiple sessions or background tasks efficiently.

### Logging
- Don't send the logs to standard out.   
- Logs go to a file: ./logs/<program_name>.log (e.g., ./logs/chatbot.log).
- Implement log rolling: Rotate logs every 10MB (use a library like github.com/natefinch/lumberjack for rotation).
- Use structured logging (e.g., with log/slog in Go stdlib).
- Logs will picked up OTEL collector automatically running locally 
- 

### Tracing and Metrics with OpenTelemetry (OTEL)
- Trace all LLM calls using OpenTelemetry (use go.opentelemetry.io/otel for tracing and metrics).
- Do not send  traces and metrics to stdout (console exporter) for simplicity; assume OTEL collector is optional.
- For each request/response:
    - Create a span for the full request cycle.
    - Record response time as an OTEL metric (e.g., histogram for latency in milliseconds).
    - From the response JSON (for all backends where applicable, especially Anthropic):
        - Extract and create OTEL metrics (gauges or counters) for each integer value under the "usage" key (e.g., input_tokens, output_tokens, cache_creation_input_tokens, etc.).
        - Handle Anthropic-specific fields like cache_creation.ephemeral_5m_input_tokens.
- Use OTEL semantic conventions for naming (e.g., http.client.request.duration).

### API Integration Details
- For Anthropic: Use the exact HTTP interface provided:
  curl https://api.anthropic.com/v1/messages \
  --header "x-api-key: $ANTHROPIC_API_KEY" \
  --header "anthropic-version: 2023-06-01" \
  --header "content-type: application/json" \
  --data '{"model": "claude-sonnet-4-20250514", "max_tokens": 1024, "messages": [{"role": "user", "content": "Hello, world"}]}'
- Parse the response JSON as shown, extracting "content" for the bot's reply and "usage" for metrics.
- For Ollama: Use local HTTP API (POST to /api/chat with JSON body similar to OpenAI format).
- For Grok and OpenAI: Use OpenAI-compatible chat completions endpoint.
- API keys loaded from environment variables (e.g., ANTHROPIC_API_KEY, OPENAI_API_KEY, GROK_API_KEY).
- Cache requests: Use a hash of the messages as key; check cache before API call, store response if miss.

### Database
- Use SQLite for persistence: Create tables for sessions (id, start_time, backend) and messages (session_id, role, content, timestamp).
- Store sessions on exit or periodically.

### Command-Line Interface
- Run as: ./chatbot [flags]
- Flags: --backend (default: ollama), --session-id (load existing), --debug (verbose logging).
- In chat: User types messages; bot responds. Support /quit, /new-session, /switch <backend>.

### Dependencies
- Only use: net/http, encoding/json, os, fmt, time, sync, github.com/mattn/go-sqlite3, github.com/natefinch/lumberjack (for log rotation), go.opentelemetry.io/otel and its submodules (trace, metric, export).

### Output
- Provide the complete, runnable Go code in a single main.go file.
- Include a README.md snippet explaining how to build/run (go build, set env vars).
- Ensure portability: No OS-specific code; use filepath.Join for paths.
- Test for efficiency: Keep it lightweight, no unnecessary allocations.

## Updates 
- Ollama configuration needs to be able to specify LLM using the following : [llm-name:version]
For example: llama3:latest or llama3:2023-06-01
- Provide more details on the default tracing provided by the application. Explain and document configuration. 
- Provide more details on the default metrics provided by the application. Explain and document configuration. 
- Provide details on configuring the application to ouput to a local OTEL collector.

I need to be able to debug the Trace output and Routing. Can you also write the traces to a log file called ./logs/chatbot_traces.log
