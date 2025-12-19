package backend

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
