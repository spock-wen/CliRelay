package handlers

import (
	"strings"

	"github.com/gin-gonic/gin"
)

func requestSessionStickyHeaderKey(c *gin.Context) string {
	if c == nil {
		return ""
	}
	for _, header := range []string{
		"Session-Id",
		"session_id",
		"X-Session-Id",
		"X-Codex-Session-Id",
		"X-Claude-Code-Session-Id",
		"Conversation-Id",
		"conversation_id",
		"X-Conversation-Id",
		"OpenAI-Conversation-Id",
	} {
		if value := strings.TrimSpace(c.GetHeader(header)); value != "" {
			return "header:" + strings.ToLower(header) + ":" + value
		}
	}
	return ""
}
