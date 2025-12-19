package telemetry

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
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

// InitLogger initializes structured logging with rotation
func InitLogger() (*slog.Logger, error) {
	logDir := "logs"
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create logs directory: %w", err)
	}

	logFile := filepath.Join(logDir, "chatbot.log")

	lumberjackLogger := &lumberjack.Logger{
		Filename:   logFile,
		MaxSize:    10, // 10 MB
		MaxBackups: 3,
		MaxAge:     28,
		Compress:   true,
	}

	// Log only to file, not to stdout
	handler := slog.NewJSONHandler(lumberjackLogger, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})

	logger := slog.New(handler)
	slog.SetDefault(logger)

	return logger, nil
}

// InitTelemetry initializes OpenTelemetry tracing and metrics
// Traces are exported to ./logs/chatbot_traces.log for debugging
// Metrics are exported to ./logs/metrics_traces.log for debugging (every 10 seconds)
// OTEL collector can still pick up traces/metrics via the SDK
func InitTelemetry(ctx context.Context) (trace.Tracer, metric.Meter, func(), error) {
	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceName("chatbot"),
			semconv.ServiceVersion("1.0.0"),
		),
	)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to create resource: %w", err)
	}

	// Create logs directory for traces
	logDir := "logs"
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return nil, nil, nil, fmt.Errorf("failed to create logs directory: %w", err)
	}

	// Set up file writer for traces with rotation
	traceFile := &lumberjack.Logger{
		Filename:   filepath.Join(logDir, "extrachat_traces_process.log"),
		MaxSize:    10, // 10 MB
		MaxBackups: 3,
		MaxAge:     28,
		Compress:   true,
	}

	// Create trace exporter that writes to file
	traceExporter, err := stdouttrace.New(
		stdouttrace.WithWriter(traceFile),
		stdouttrace.WithPrettyPrint(),
	)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to create trace exporter: %w", err)
	}

	// Set up tracer provider with file exporter
	// OTEL collector can still pick up traces via the SDK
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(traceExporter),
		sdktrace.WithResource(res),
	)
	otel.SetTracerProvider(tp)

	// Set up file writer for metrics with rotation
	metricsFile := &lumberjack.Logger{
		Filename:   filepath.Join(logDir, "extrachat_metrics_process.log"),
		MaxSize:    10, // 10 MB
		MaxBackups: 3,
		MaxAge:     28,
		Compress:   true,
	}

	// Create metrics exporter that writes to file
	metricExporter, err := stdoutmetric.New(
		stdoutmetric.WithWriter(metricsFile),
		stdoutmetric.WithPrettyPrint(),
	)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to create metric exporter: %w", err)
	}

	// Set up meter provider with file exporter
	// OTEL collector can still pick up metrics via the SDK
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
		if err := traceFile.Close(); err != nil {
			slog.Error("failed to close trace file", "error", err)
		}
		if err := metricsFile.Close(); err != nil {
			slog.Error("failed to close metrics file", "error", err)
		}
	}

	return tracer, meter, cleanup, nil
}

// InitDB initializes the SQLite database
func InitDB() (*sql.DB, error) {
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
