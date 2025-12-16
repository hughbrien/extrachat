# OpenTelemetry Configuration Guide

This document provides detailed information about the OpenTelemetry (OTEL) instrumentation in the chatbot application, including tracing, metrics, and how to configure a local OTEL collector.

## Table of Contents

- [Overview](#overview)
- [Default Tracing Configuration](#default-tracing-configuration)
- [Default Metrics Configuration](#default-metrics-configuration)
- [OTEL Collector Setup](#otel-collector-setup)
- [Viewing Telemetry Data](#viewing-telemetry-data)
- [Troubleshooting](#troubleshooting)

## Overview

The chatbot application is fully instrumented with OpenTelemetry to provide observability into:
- **Traces**: Request/response cycles, latency, and distributed tracing
- **Metrics**: Token usage, cache hit rates, API call durations, and more

**Trace Output:**
- Traces are automatically written to `./logs/chatbot_traces.log` in JSON format for local debugging
- File uses automatic rotation (10MB limit, 3 backups, compressed)
- Each trace includes full span details, timings, and attributes
- Traces are NOT written to stdout to keep the console clean

**Metrics Output:**
- Metrics are automatically written to `./logs/metrics_traces.log` in JSON format for local debugging
- Exported every 10 seconds with current metric values
- File uses automatic rotation (10MB limit, 3 backups, compressed)
- Includes all LLM usage metrics, request durations, and custom metrics
- Metrics are NOT written to stdout to keep the console clean

**OTEL Collector Integration:**
The application is designed to work with an OpenTelemetry Collector running locally, which can automatically pick up and export telemetry data to your preferred backend (e.g., Jaeger, Prometheus, Grafana, etc.).

## Default Tracing Configuration

### Automatic Instrumentation

The chatbot automatically creates traces for all LLM API calls. Each trace includes:

**Trace Attributes:**
- Service name: `chatbot`
- Service version: `1.0.0`
- Trace ID: Unique identifier for the entire request flow
- Span ID: Unique identifier for each operation

**Spans Created:**

1. **`anthropic_api_call`** - Anthropic Claude API requests
   - Duration: Total time for API call
   - Includes network latency and response processing

2. **`ollama_api_call`** - Ollama local model requests
   - Duration: Total time for local model inference
   - Model name recorded as span attribute

3. **`grok_api_call`** - xAI Grok API requests
   - Duration: Total time for API call
   - Includes authentication and request processing

4. **`openai_api_call`** - OpenAI API requests
   - Duration: Total time for API call
   - Model and completion details

### Trace Data Structure

```
Trace (Request ID: abc123)
└── Span: anthropic_api_call
    ├── Start Time: 2025-12-16T10:30:00Z
    ├── End Time: 2025-12-16T10:30:02Z
    ├── Duration: 2.1s
    └── Attributes:
        ├── service.name: chatbot
        ├── service.version: 1.0.0
        ├── http.method: POST
        └── http.status_code: 200
```

### Trace Propagation

The application uses W3C Trace Context propagation by default, making it compatible with distributed tracing systems. If you're running multiple services, trace context is automatically propagated across service boundaries.

### Viewing Trace Files for Debugging

Traces are automatically written to `./logs/chatbot_traces.log` in JSON format. Each trace entry contains:
- Complete span information
- Trace and span IDs
- Timestamps and durations
- Resource attributes
- Status and error information

**Example Trace Entry:**
```json
{
  "Name": "anthropic_api_call",
  "SpanContext": {
    "TraceID": "a1b2c3d4e5f6789012345678901234567",
    "SpanID": "1234567890abcdef",
    "TraceFlags": "01",
    "TraceState": "",
    "Remote": false
  },
  "Parent": {
    "TraceID": "00000000000000000000000000000000",
    "SpanID": "0000000000000000",
    "TraceFlags": "00",
    "TraceState": "",
    "Remote": false
  },
  "StartTime": "2025-12-16T10:30:00.123456789-05:00",
  "EndTime": "2025-12-16T10:30:02.234567890-05:00",
  "Attributes": [
    {
      "Key": "service.name",
      "Value": {
        "Type": "STRING",
        "Value": "chatbot"
      }
    }
  ],
  "Status": {
    "Code": "Ok",
    "Description": ""
  }
}
```

**Viewing Traces:**
```bash
# View latest traces
tail -f ./logs/chatbot_traces.log

# Pretty print JSON traces
cat ./logs/chatbot_traces.log | jq '.'

# Search for specific spans
grep "anthropic_api_call" ./logs/chatbot_traces.log | jq '.'

# Filter by status (errors only)
cat ./logs/chatbot_traces.log | jq 'select(.Status.Code == "Error")'

# View traces with specific trace ID
cat ./logs/chatbot_traces.log | jq 'select(.SpanContext.TraceID == "YOUR_TRACE_ID")'
```

**Rotating Trace Files:**
The trace file automatically rotates when it reaches 10MB. Old files are compressed and up to 3 backups are kept.

## Default Metrics Configuration

### Automatic Metrics Collection

The chatbot collects various metrics to provide insight into application performance and LLM usage.

### HTTP Request Metrics

**Metric Name**: `http.client.request.duration`
**Type**: Histogram
**Unit**: Milliseconds
**Description**: Measures the duration of HTTP requests to LLM APIs

**Labels/Dimensions**:
- Backend: `ollama`, `anthropic`, `grok`, or `openai`
- Status: HTTP status code

**Example Values**:
```
http.client.request.duration{backend="anthropic"} = [150ms, 200ms, 175ms, ...]
http.client.request.duration{backend="ollama"} = [500ms, 520ms, 480ms, ...]
```

### LLM Usage Metrics

The application automatically extracts and records all usage metrics from LLM API responses.

#### Anthropic Usage Metrics

**Base Metrics**:
- `llm.usage.input_tokens` - Number of input tokens processed
- `llm.usage.output_tokens` - Number of tokens generated
- `llm.usage.cache_creation_input_tokens` - Tokens used for cache creation
- `llm.usage.cache_read_input_tokens` - Tokens read from cache

**Anthropic-Specific Metrics**:
- `llm.usage.cache_creation.ephemeral_5m_input_tokens` - Ephemeral cache (5-minute TTL) tokens
- `llm.usage.cache_creation.ephemeral_1h_input_tokens` - Ephemeral cache (1-hour TTL) tokens

**Example JSON Response**:
```json
{
  "usage": {
    "input_tokens": 256,
    "output_tokens": 512,
    "cache_creation_input_tokens": 0,
    "cache_read_input_tokens": 128
  }
}
```

#### OpenAI/Grok Usage Metrics

**Metrics**:
- `llm.usage.prompt_tokens` - Tokens in the prompt
- `llm.usage.completion_tokens` - Tokens in the completion
- `llm.usage.total_tokens` - Total tokens used

### Cache Performance Metrics

Cache hits and misses are logged (available in log analysis):
- Cache key generation using SHA-256
- Cache lookup timing
- Cache storage operations

**Log Example**:
```json
{
  "time": "2025-12-16T10:30:00Z",
  "level": "INFO",
  "msg": "cache hit",
  "key": "a1b2c3d4e5f67890"
}
```

### Viewing Metrics Files for Debugging

Metrics are automatically written to `./logs/metrics_traces.log` in JSON format every 10 seconds. Each metrics export contains:
- Resource attributes (service name, version)
- Scope information
- All recorded metrics with their values
- Timestamps
- Metric types (Histogram, Counter, Gauge, etc.)

**Example Metrics Entry:**
```json
{
  "Resource": [
    {
      "Key": "service.name",
      "Value": {
        "Type": "STRING",
        "Value": "chatbot"
      }
    },
    {
      "Key": "service.version",
      "Value": {
        "Type": "STRING",
        "Value": "1.0.0"
      }
    }
  ],
  "ScopeMetrics": [
    {
      "Scope": {
        "Name": "chatbot",
        "Version": "",
        "SchemaURL": ""
      },
      "Metrics": [
        {
          "Name": "http.client.request.duration",
          "Description": "HTTP request duration in milliseconds",
          "Unit": "ms",
          "Data": {
            "DataPoints": [
              {
                "Attributes": [],
                "StartTime": "2025-12-16T10:30:00.123456789-05:00",
                "Time": "2025-12-16T10:30:10.123456789-05:00",
                "Count": 15,
                "Sum": 3250.5,
                "Min": 150.2,
                "Max": 450.8
              }
            ],
            "Temporality": "CumulativeTemporality"
          }
        },
        {
          "Name": "llm.usage.input_tokens",
          "Description": "LLM usage metric: input_tokens",
          "Unit": "",
          "Data": {
            "DataPoints": [
              {
                "Attributes": [],
                "StartTime": "2025-12-16T10:30:00.123456789-05:00",
                "Time": "2025-12-16T10:30:10.123456789-05:00",
                "Value": 2048
              }
            ],
            "Temporality": "CumulativeTemporality",
            "IsMonotonic": true
          }
        }
      ]
    }
  ]
}
```

**Viewing Metrics:**
```bash
# View latest metrics
tail -f ./logs/metrics_traces.log

# Pretty print JSON metrics
cat ./logs/metrics_traces.log | jq '.'

# Extract specific metric
cat ./logs/metrics_traces.log | jq '.ScopeMetrics[].Metrics[] | select(.Name == "http.client.request.duration")'

# Get all LLM usage metrics
cat ./logs/metrics_traces.log | jq '.ScopeMetrics[].Metrics[] | select(.Name | startswith("llm.usage"))'

# Show latest metric values (last export)
tail -1 ./logs/metrics_traces.log | jq '.ScopeMetrics[].Metrics[] | {name: .Name, value: .Data.DataPoints[0].Value}'

# Calculate average request duration
cat ./logs/metrics_traces.log | jq '.ScopeMetrics[].Metrics[] | select(.Name == "http.client.request.duration") | .Data.DataPoints[0] | .Sum / .Count'
```

**Monitoring Metrics Over Time:**
```bash
# Watch metrics in real-time (updates every 2 seconds)
watch -n 2 'tail -1 ./logs/metrics_traces.log | jq ".ScopeMetrics[].Metrics[]"'

# Track token usage over time
grep "llm.usage.input_tokens" ./logs/metrics_traces.log | jq -r '.ScopeMetrics[].Metrics[] | select(.Name == "llm.usage.input_tokens") | "\(.Data.DataPoints[0].Time): \(.Data.DataPoints[0].Value)"'
```

**Rotating Metrics Files:**
The metrics file automatically rotates when it reaches 10MB. Old files are compressed and up to 3 backups are kept.

## OTEL Collector Setup

### Installing the OTEL Collector

The OpenTelemetry Collector is a vendor-agnostic way to receive, process, and export telemetry data.

#### Option 1: Docker (Recommended)

```bash
# Pull the OTEL Collector image
docker pull otel/opentelemetry-collector:latest

# Run the collector
docker run -d \
  --name otel-collector \
  -p 4317:4317 \
  -p 4318:4318 \
  -p 55679:55679 \
  -v $(pwd)/otel-collector-config.yaml:/etc/otel-collector-config.yaml \
  otel/opentelemetry-collector:latest \
  --config=/etc/otel-collector-config.yaml
```

#### Option 2: Direct Installation

**macOS** (using Homebrew):
```bash
brew install opentelemetry-collector
```

**Linux**:
```bash
curl -LO https://github.com/open-telemetry/opentelemetry-collector-releases/releases/download/v0.91.0/otelcol_0.91.0_linux_amd64.tar.gz
tar -xzf otelcol_0.91.0_linux_amd64.tar.gz
sudo mv otelcol /usr/local/bin/
```

**Windows**:
Download the executable from [OpenTelemetry Releases](https://github.com/open-telemetry/opentelemetry-collector-releases/releases)

### Collector Configuration

Create a file `otel-collector-config.yaml`:

```yaml
receivers:
  # OTLP receiver for traces and metrics
  otlp:
    protocols:
      grpc:
        endpoint: 0.0.0.0:4317
      http:
        endpoint: 0.0.0.0:4318

processors:
  # Batch processor for better performance
  batch:
    timeout: 10s
    send_batch_size: 1024

  # Memory limiter to prevent OOM
  memory_limiter:
    check_interval: 1s
    limit_mib: 512

exporters:
  # Export to console for debugging
  logging:
    loglevel: debug

  # Export to Jaeger for traces
  jaeger:
    endpoint: localhost:14250
    tls:
      insecure: true

  # Export to Prometheus for metrics
  prometheus:
    endpoint: "0.0.0.0:8889"

  # Export to file for local storage
  file:
    path: ./telemetry-output.json

service:
  pipelines:
    traces:
      receivers: [otlp]
      processors: [memory_limiter, batch]
      exporters: [logging, jaeger, file]

    metrics:
      receivers: [otlp]
      processors: [memory_limiter, batch]
      exporters: [logging, prometheus, file]
```

### Running the Collector

```bash
# With Docker
docker start otel-collector

# With local installation
otelcol --config otel-collector-config.yaml
```

### Connecting the Chatbot to the Collector

The chatbot automatically connects to the OTEL collector using the standard OTLP endpoints:

**Environment Variables** (optional, for custom configuration):
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

**Note**: The current implementation uses the OTEL SDK's default configuration, which automatically discovers the collector running locally. For production use, you may need to update the code to use OTLP exporters explicitly.

## Viewing Telemetry Data

### Traces with Jaeger

1. Install and run Jaeger:
```bash
docker run -d --name jaeger \
  -p 5775:5775/udp \
  -p 6831:6831/udp \
  -p 6832:6832/udp \
  -p 5778:5778 \
  -p 16686:16686 \
  -p 14250:14250 \
  -p 14268:14268 \
  -p 14269:14269 \
  -p 9411:9411 \
  jaegertracing/all-in-one:latest
```

2. Open Jaeger UI: http://localhost:16686
3. Select service: `chatbot`
4. View traces for each LLM API call

### Metrics with Prometheus & Grafana

1. Install Prometheus:
```bash
docker run -d --name prometheus \
  -p 9090:9090 \
  -v $(pwd)/prometheus.yml:/etc/prometheus/prometheus.yml \
  prom/prometheus
```

2. Configure Prometheus (`prometheus.yml`):
```yaml
scrape_configs:
  - job_name: 'chatbot'
    scrape_interval: 10s
    static_configs:
      - targets: ['localhost:8889']
```

3. Install Grafana:
```bash
docker run -d --name grafana \
  -p 3000:3000 \
  grafana/grafana
```

4. Add Prometheus as data source in Grafana
5. Create dashboards for:
   - API request duration
   - Token usage over time
   - Cache hit rate
   - Error rates

### Viewing Logs

Logs are stored in `./logs/chatbot.log` in JSON format, which can be ingested by log aggregators like:
- ELK Stack (Elasticsearch, Logstash, Kibana)
- Grafana Loki
- Splunk
- Datadog

**Example Logstash Configuration**:
```ruby
input {
  file {
    path => "/path/to/logs/chatbot.log"
    codec => json
  }
}

output {
  elasticsearch {
    hosts => ["localhost:9200"]
    index => "chatbot-logs-%{+YYYY.MM.dd}"
  }
}
```

## Troubleshooting

### No Traces Appearing in Jaeger

**Issue**: Traces are not showing up in Jaeger UI

**Solutions**:
1. Verify OTEL collector is running:
   ```bash
   docker ps | grep otel-collector
   ```

2. Check collector logs:
   ```bash
   docker logs otel-collector
   ```

3. Verify Jaeger is running:
   ```bash
   docker ps | grep jaeger
   ```

4. Check network connectivity:
   ```bash
   curl http://localhost:4317
   ```

### Metrics Not Appearing in Prometheus

**Issue**: Metrics endpoint not responding

**Solutions**:
1. Check OTEL collector Prometheus exporter config
2. Verify endpoint is accessible:
   ```bash
   curl http://localhost:8889/metrics
   ```

3. Check Prometheus scrape config matches the collector's exposed port

### High Memory Usage

**Issue**: OTEL collector consuming too much memory

**Solutions**:
1. Adjust `memory_limiter` in collector config:
   ```yaml
   processors:
     memory_limiter:
       limit_mib: 256  # Reduce from 512
   ```

2. Increase batch size to reduce processing overhead:
   ```yaml
   processors:
     batch:
       send_batch_size: 2048  # Increase from 1024
   ```

### Logs Not Appearing

**Issue**: No logs in `./logs/chatbot.log`

**Solutions**:
1. Check directory permissions:
   ```bash
   ls -la logs/
   ```

2. Check disk space:
   ```bash
   df -h
   ```

3. Enable debug logging:
   ```bash
   ./chatbot --debug
   ```

## Advanced Configuration

### Custom Exporters

To use custom OTLP exporters, you'll need to modify the `initTelemetry` function in `main.go`:

```go
import (
    "go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
    "go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
)

// In initTelemetry function:
traceExporter, err := otlptracegrpc.New(ctx,
    otlptracegrpc.WithEndpoint("localhost:4317"),
    otlptracegrpc.WithInsecure(),
)

metricExporter, err := otlpmetricgrpc.New(ctx,
    otlpmetricgrpc.WithEndpoint("localhost:4317"),
    otlpmetricgrpc.WithInsecure(),
)
```

### Sampling

To reduce telemetry overhead, implement sampling:

```go
tp := sdktrace.NewTracerProvider(
    sdktrace.WithSampler(sdktrace.TraceIDRatioBased(0.1)), // Sample 10%
    sdktrace.WithResource(res),
)
```

## References

- [OpenTelemetry Documentation](https://opentelemetry.io/docs/)
- [OTEL Collector Configuration](https://opentelemetry.io/docs/collector/configuration/)
- [Jaeger Documentation](https://www.jaegertracing.io/docs/)
- [Prometheus Documentation](https://prometheus.io/docs/)
- [Go OTEL SDK](https://pkg.go.dev/go.opentelemetry.io/otel)
