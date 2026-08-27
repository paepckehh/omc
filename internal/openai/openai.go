// Package openai implements the optional LLM step of omc against any
// OpenAI-compatible REST API endpoint (anything that speaks the /chat/completions
// and /models routes). It is the fallback backend used when Ollama is not
// configured or unreachable.
//
// The prompts are shared with the Ollama backend via the internal/llm package
// so both backends produce comparable commit messages.
package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"paepcke.de/omc/internal/llm"
)

// DefaultModel is used when OPENAPI_MODEL (or OPENAPI_DESC_MODEL) is empty.
const DefaultModel = "gpt-3.5-turbo"

// Client talks to a single OpenAI-compatible REST API instance. It
// implements llm.Generator.
type Client struct {
	baseURL string
	model   string
	token   string
	http    *http.Client
}

// New creates a Client. model may be empty (DefaultModel applies); token may
// be empty for endpoints that do not require authentication.
func New(baseURL, model, token string) *Client {
	if model == "" {
		model = DefaultModel
	}
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		model:   model,
		token:   token,
		http:    &http.Client{Timeout: 120 * time.Second},
	}
}

// Available probes the endpoint's /models route and reports whether a working
// OpenAI-compatible instance answers at the configured URL. The bearer token
// (when set) is sent as Authorization, matching the chat-completions call.
func (c *Client) Available(ctx context.Context) bool {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/models", nil)
	if err != nil {
		return false
	}
	c.setAuth(req)
	resp, err := c.http.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	return resp.StatusCode == http.StatusOK
}

// DescribeDetail returns a detailed, conversational commit message explaining
// the staged diff.
func (c *Client) DescribeDetail(ctx context.Context, diff string, diffStat string) (string, error) {
	return c.chat(ctx, llm.DetailPrompt(diff, diffStat))
}

// SummarizeTLDR condenses a detailed commit message into a single
// TL;DR-style line suitable as a git commit subject.
func (c *Client) SummarizeTLDR(ctx context.Context, detail string) (string, error) {
	return c.chat(ctx, llm.TLDRPrompt(detail))
}

// setAuth adds the bearer token (when configured) to the request.
func (c *Client) setAuth(req *http.Request) {
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
}

// chat sends a single non-streaming chat-completions request with the prompt
// as the sole user message and returns the model's text.
func (c *Client) chat(ctx context.Context, prompt string) (string, error) {
	payload := map[string]any{
		"model":       c.model,
		"temperature": 0.3,
		"messages": []map[string]string{
			{"role": "user", "content": prompt},
		},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("encode request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	c.setAuth(req)

	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("openai request: %w", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return "", fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("openai %s: %s", resp.Status, strings.TrimSpace(string(data)))
	}

	var out struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return "", fmt.Errorf("parse response: %w", err)
	}
	if len(out.Choices) == 0 {
		return "", fmt.Errorf("openai response had no choices")
	}
	return strings.TrimSpace(out.Choices[0].Message.Content), nil
}
