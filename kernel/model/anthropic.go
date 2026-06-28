package model

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Anthropic implements Model against the Claude Messages API. Used for agents
// where reasoning quality matters most (PR writing, behavioral critique).
type Anthropic struct {
	apiKey string
	model  string
	http   *http.Client
}

func NewAnthropic(apiKey, model string) *Anthropic {
	if model == "" {
		model = "claude-opus-4-8"
	}
	return &Anthropic{
		apiKey: apiKey,
		model:  model,
		http:   &http.Client{Timeout: 120 * time.Second},
	}
}

func (a *Anthropic) Name() string { return a.model }

type anthropicReq struct {
	Model     string    `json:"model"`
	MaxTokens int       `json:"max_tokens"`
	System    string    `json:"system,omitempty"`
	Messages  []anthMsg `json:"messages"`
	Temp      float32   `json:"temperature,omitempty"`
}

type anthMsg struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

func (a *Anthropic) Complete(ctx context.Context, req Request) (*Response, error) {
	maxTok := req.MaxTokens
	if maxTok == 0 {
		maxTok = 4096
	}

	// For structured output we instruct via the system prompt + a final
	// assistant prefill of "{" to strongly bias JSON. (Anthropic's tool API is
	// the heavier-duty path; this prefill technique is reliable for single
	// JSON objects and keeps the provider code small.)
	sys := req.System
	msgs := make([]anthMsg, 0, len(req.Messages)+1)
	for _, m := range req.Messages {
		msgs = append(msgs, anthMsg{Role: m.Role, Content: m.Content})
	}
	prefill := false
	if req.JSONSchema != nil {
		sys += "\n\nRespond ONLY with a single JSON object conforming to this schema. No prose, no markdown fences:\n" + string(req.JSONSchema)
		msgs = append(msgs, anthMsg{Role: "assistant", Content: "{"})
		prefill = true
	}

	payload, _ := json.Marshal(anthropicReq{
		Model:     a.model,
		MaxTokens: maxTok,
		System:    sys,
		Messages:  msgs,
		Temp:      req.Temperature,
	})

	httpReq, _ := http.NewRequestWithContext(ctx, http.MethodPost,
		"https://api.anthropic.com/v1/messages", bytes.NewReader(payload))
	httpReq.Header.Set("x-api-key", a.apiKey)
	httpReq.Header.Set("anthropic-version", "2023-06-01")
	httpReq.Header.Set("content-type", "application/json")

	resp, err := a.http.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("anthropic request: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("anthropic failed (%d): %s", resp.StatusCode, raw)
	}

	var out struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
		Usage struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("decode anthropic: %w", err)
	}
	text := ""
	if len(out.Content) > 0 {
		text = out.Content[0].Text
	}
	// Re-attach the "{" we prefilled so callers get valid JSON.
	if prefill {
		text = "{" + text
	}

	return &Response{
		Text:         text,
		InputTokens:  out.Usage.InputTokens,
		OutputTokens: out.Usage.OutputTokens,
		Model:        a.model,
	}, nil
}
