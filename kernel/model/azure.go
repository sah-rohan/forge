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

// AzureOpenAI implements Model against an Azure OpenAI deployment. Used for
// cheaper, high-volume structured-output agents (skill graphs, problem recs)
// and to keep inference in the same Azure region as the rest of the stack.
type AzureOpenAI struct {
	endpoint   string // https://<resource>.openai.azure.com
	deployment string // deployment name
	apiVersion string
	apiKey     string
	http       *http.Client
}

func NewAzureOpenAI(endpoint, deployment, apiKey string) *AzureOpenAI {
	return &AzureOpenAI{
		endpoint:   endpoint,
		deployment: deployment,
		apiVersion: "2024-08-01-preview",
		apiKey:     apiKey,
		http:       &http.Client{Timeout: 120 * time.Second},
	}
}

func (a *AzureOpenAI) Name() string { return "azure/" + a.deployment }

func (a *AzureOpenAI) Complete(ctx context.Context, req Request) (*Response, error) {
	maxTok := req.MaxTokens
	if maxTok == 0 {
		maxTok = 4096
	}

	msgs := make([]map[string]string, 0, len(req.Messages)+1)
	if req.System != "" {
		msgs = append(msgs, map[string]string{"role": "system", "content": req.System})
	}
	for _, m := range req.Messages {
		msgs = append(msgs, map[string]string{"role": m.Role, "content": m.Content})
	}

	body := map[string]any{
		"messages":    msgs,
		"max_tokens":  maxTok,
		"temperature": req.Temperature,
	}
	// Azure OpenAI supports JSON mode; when a schema is requested we ask for a
	// JSON object (schema is enforced by validation in the render layer).
	if req.JSONSchema != nil {
		body["response_format"] = map[string]string{"type": "json_object"}
	}
	payload, _ := json.Marshal(body)

	url := fmt.Sprintf("%s/openai/deployments/%s/chat/completions?api-version=%s",
		a.endpoint, a.deployment, a.apiVersion)
	httpReq, _ := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	httpReq.Header.Set("api-key", a.apiKey)
	httpReq.Header.Set("content-type", "application/json")

	resp, err := a.http.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("azure request: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("azure failed (%d): %s", resp.StatusCode, raw)
	}

	var out struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("decode azure: %w", err)
	}
	text := ""
	if len(out.Choices) > 0 {
		text = out.Choices[0].Message.Content
	}
	return &Response{
		Text:         text,
		InputTokens:  out.Usage.PromptTokens,
		OutputTokens: out.Usage.CompletionTokens,
		Model:        a.Name(),
	}, nil
}
