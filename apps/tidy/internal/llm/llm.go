// Package llm wraps the OpenAI-compatible chat/completions endpoint.
package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type Client struct {
	BaseURL string
	APIKey  string
	Model   string
	hc      *http.Client
}

func New(baseURL, apiKey, model string) *Client {
	return &Client{
		BaseURL: strings.TrimRight(baseURL, "/"),
		APIKey:  apiKey,
		Model:   model,
		hc:      &http.Client{Timeout: 5 * time.Minute},
	}
}

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatReq struct {
	Model          string                 `json:"model"`
	Messages       []Message              `json:"messages"`
	Temperature    float32                `json:"temperature"`
	MaxTokens      int                    `json:"max_tokens,omitempty"`
	ResponseFormat map[string]interface{} `json:"response_format,omitempty"`
}

type chatResp struct {
	Choices []struct {
		Message Message `json:"message"`
	} `json:"choices"`
	Error struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error,omitempty"`
}

// Chat sends a chat-completion request, asking for a JSON-object response so
// providers that support response_format will return clean JSON. Falls back to
// best-effort string parsing when they don't.
func (c *Client) Chat(ctx context.Context, system, user string, maxTokens int) (string, error) {
	req := chatReq{
		Model: c.Model,
		Messages: []Message{
			{Role: "system", Content: system},
			{Role: "user", Content: user},
		},
		Temperature: 0.2,
		MaxTokens:   maxTokens,
		// Most OpenAI-compatible providers (DeepSeek included) honour this hint;
		// those that don't will simply ignore it.
		ResponseFormat: map[string]interface{}{"type": "json_object"},
	}
	body, _ := json.Marshal(req)

	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.BaseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.APIKey)

	resp, err := c.hc.Do(httpReq)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	rawBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("llm: %s: %s", resp.Status, string(rawBody))
	}
	var cr chatResp
	if err := json.Unmarshal(rawBody, &cr); err != nil {
		return "", fmt.Errorf("llm: bad response: %v: %s", err, string(rawBody))
	}
	if cr.Error.Message != "" {
		return "", fmt.Errorf("llm: %s", cr.Error.Message)
	}
	if len(cr.Choices) == 0 {
		return "", fmt.Errorf("llm: empty choices")
	}
	return cr.Choices[0].Message.Content, nil
}
