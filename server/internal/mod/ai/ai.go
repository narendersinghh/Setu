// Package ai: AI providers (Claude, ChatGPT, OpenRouter, Gemini, Groq, DeepSeek, custom), usage & cost.
// OWNER: ai-engine module. The exported API below is a contract used by other modules – keep it.
package ai

import (
	"context"
	"errors"

	"github.com/narendersinghh/setu/server/internal/app"
	"github.com/narendersinghh/setu/server/internal/mod/shared"
	"github.com/narendersinghh/setu/server/internal/settings"
)

type Msg struct {
	Role    string `json:"role"` // user | assistant
	Content string `json:"content"`
}

type Request struct {
	System      string
	Messages    []Msg
	MaxTokens   int
	Temperature float64
	Model       string // optional override of the workspace model
	WebSearch   bool   // live web research (Claude web search tool) when the provider supports it
	JSON        bool   // ask for a JSON answer
	Purpose     string // usage op label, e.g. "report", "audit", "research"
	Trace       *shared.Trace
	OnDelta     func(text string) // streaming progress (optional)
}

type Source struct {
	Title string `json:"title"`
	URL   string `json:"url"`
}

type Response struct {
	Text      string
	Model     string
	TokensIn  int
	TokensOut int
	Cost      float64
	Sources   []Source
}

// Complete sends one request with the workspace's AI connection, records usage and the process step.
func Complete(ctx context.Context, a *app.App, orgID int64, s *settings.Store, r Request) (*Response, error) {
	return nil, errors.New("ai: not implemented")
}

// Configured reports whether the workspace has an AI connection.
func Configured(s *settings.Store) bool { return false }

func Register(a *app.App) {}
