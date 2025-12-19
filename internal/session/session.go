package session

import "time"

// Message represents a single chat message
type Message struct {
	Role      string    `json:"role"`
	Content   string    `json:"content"`
	Timestamp time.Time `json:"timestamp"`
}

// Session represents a chat session
type Session struct {
	ID        string    `json:"id"`
	StartTime time.Time `json:"start_time"`
	Backend   string    `json:"backend"`
	Messages  []Message `json:"messages"`
}
