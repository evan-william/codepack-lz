package tokens

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	DefaultAnthropicModel    = "claude-sonnet-4-20250514"
	DefaultAnthropicEndpoint = "https://api.anthropic.com/v1/messages/count_tokens"
	AnthropicVersion         = "2023-06-01"
)

// NewAnthropicCounter returns a provider-exact counter backed by Anthropic's
// Messages count-tokens endpoint. The API key must come from the caller so it
// never appears in output or logs.
func NewAnthropicCounter(apiKey, model, endpoint string) (Counter, error) {
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		return nil, fmt.Errorf("ANTHROPIC_API_KEY is required for --count-tokens=api")
	}
	if strings.TrimSpace(model) == "" {
		model = DefaultAnthropicModel
	}
	if strings.TrimSpace(endpoint) == "" {
		endpoint = DefaultAnthropicEndpoint
	}
	return &anthropicCounter{
		apiKey:   apiKey,
		model:    model,
		endpoint: endpoint,
		client:   &http.Client{Timeout: 30 * time.Second},
	}, nil
}

type anthropicCounter struct {
	apiKey   string
	model    string
	endpoint string
	client   *http.Client
}

func (c *anthropicCounter) Name() string   { return "anthropic:" + c.model }
func (c *anthropicCounter) Estimate() bool { return false }

func (c *anthropicCounter) Count(text []byte) (int, error) {
	body := struct {
		Model    string `json:"model"`
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}{
		Model: c.model,
		Messages: []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		}{{Role: "user", Content: string(text)}},
	}

	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(body); err != nil {
		return 0, err
	}

	req, err := http.NewRequest(http.MethodPost, c.endpoint, &buf)
	if err != nil {
		return 0, err
	}
	req.Header.Set("content-type", "application/json")
	req.Header.Set("x-api-key", c.apiKey)
	req.Header.Set("anthropic-version", AnthropicVersion)

	resp, err := c.client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return 0, fmt.Errorf("anthropic count-tokens failed: %s: %s", resp.Status, strings.TrimSpace(string(data)))
	}

	var out struct {
		InputTokens int `json:"input_tokens"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return 0, err
	}
	if out.InputTokens < 0 {
		return 0, fmt.Errorf("anthropic count-tokens returned negative input_tokens")
	}
	return out.InputTokens, nil
}
