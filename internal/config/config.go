package config

const (
	BackendOllama    = "ollama"
	BackendAnthropic = "anthropic"
	BackendGrok      = "grok"
	BackendOpenAI    = "openai"
)

// Config holds application configuration
type Config struct {
	Backend     string
	SessionID   string
	Debug       bool
	OllamaModel string // Model specification in format "model:version" (e.g., "llama3:latest")

	// MCP Configuration
	MCPEnabled       bool     // Enable MCP tool support
	MCPLocalServers  []string // Paths to Python MCP servers
	MCPRemoteServers []string // URLs to remote MCP servers (http:// or ws://)
}
