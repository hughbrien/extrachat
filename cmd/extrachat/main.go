package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"ExtraChat/internal/chatbot"
	"ExtraChat/internal/config"
)

func main() {
	var cfg config.Config
	var mcpLocalServers string
	var mcpRemoteServers string

	flag.StringVar(&cfg.Backend, "backend", config.BackendOllama, "LLM backend (ollama|anthropic|grok|openai)")
	flag.StringVar(&cfg.SessionID, "session-id", "", "Load existing session by ID")
	flag.BoolVar(&cfg.Debug, "debug", false, "Enable debug logging")
	flag.StringVar(&cfg.OllamaModel, "ollama-model", "llama3:latest", "Ollama model specification (format: model:version)")

	// MCP flags
	flag.BoolVar(&cfg.MCPEnabled, "mcp-enabled", false, "Enable MCP tool support")
	flag.StringVar(&mcpLocalServers, "mcp-local", "", "Comma-separated paths to Python MCP servers")
	flag.StringVar(&mcpRemoteServers, "mcp-remote", "", "Comma-separated URLs to remote MCP servers")

	flag.Parse()

	// Parse comma-separated MCP servers
	if mcpLocalServers != "" {
		cfg.MCPLocalServers = strings.Split(mcpLocalServers, ",")
	}
	if mcpRemoteServers != "" {
		cfg.MCPRemoteServers = strings.Split(mcpRemoteServers, ",")
	}

	bot, err := chatbot.NewChatBot(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to initialize chatbot: %v\n", err)
		os.Exit(1)
	}

	if err := bot.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
