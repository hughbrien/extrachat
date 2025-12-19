package cache

import (
	"crypto/sha256"
	"fmt"
	"time"

	"ExtraChat/internal/session"
)

// CachedResponse represents a cached API response
type CachedResponse struct {
	Response  string
	Timestamp time.Time
}

// GenerateCacheKey generates a cache key from messages
func GenerateCacheKey(messages []session.Message) string {
	h := sha256.New()
	for _, msg := range messages {
		h.Write([]byte(msg.Role))
		h.Write([]byte(msg.Content))
	}
	return fmt.Sprintf("%x", h.Sum(nil))
}
