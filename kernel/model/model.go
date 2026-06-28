// Package model is the kernel's one-interface-many-providers abstraction over
// LLM inference. Agents depend only on the Model interface, never on a vendor.
// This is what lets a PR-writing agent run on Claude while a cheap JSON-shaping
// agent runs on Azure OpenAI — chosen per agent, swappable without touching
// agent logic.
package model

import "context"

// Message is one turn in a conversation. Role is "system" | "user" |
// "assistant".
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// Request is a single inference call. When JSONSchema is non-nil the provider
// is asked to return output conforming to it (structured output / tool-forced
// JSON) — this is how the kernel guarantees the render layer always gets
// machine-parseable results instead of prose.
type Request struct {
	System      string
	Messages    []Message
	JSONSchema  []byte // optional: forces structured JSON output
	MaxTokens   int
	Temperature float32
}

// Response carries the raw text the model produced plus usage for the budget
// governor. For structured calls, Text is the JSON string.
type Response struct {
	Text         string
	InputTokens  int
	OutputTokens int
	Model        string
}

// Model is the single seam every agent and every provider implements/uses.
type Model interface {
	// Name identifies the underlying model (e.g. "claude-opus-4-8",
	// "azure/gpt-4o-mini") for logging + cost attribution.
	Name() string
	// Complete runs one inference. Implementations must honor ctx cancellation.
	Complete(ctx context.Context, req Request) (*Response, error)
}
