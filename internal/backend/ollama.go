package backend

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

// OllamaTagsResponse represents the response from Ollama /api/tags endpoint
type OllamaTagsResponse struct {
	Models []OllamaModel `json:"models"`
}

// OllamaModel represents a single model in the Ollama tags response
type OllamaModel struct {
	Name       string `json:"name"`
	ModifiedAt string `json:"modified_at"`
	Size       int64  `json:"size"`
	Digest     string `json:"digest"`
}
