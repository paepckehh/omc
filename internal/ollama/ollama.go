// Package ollama implements the optional LLM step of omc. When
// OLLAMA_DESC_URL points at a reachable local Ollama REST API, the staged
// diff is sent twice: once to generate a detailed commit description, then
// the description itself is condensed into a TL;DR summary.
package ollama

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

// DefaultModel is used when OLLAMA_DESC_MODEL is empty.
const DefaultModel = "llama3.2"

// Client talks to a single Ollama REST API instance. It implements llm.Generator.
type Client struct {
	baseURL string
	model   string
	http    *http.Client
}

// New creates a Client. model may be empty; the Ollama default applies.
func New(baseURL, model string) *Client {
	if model == "" {
		model = DefaultModel
	}
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		model:   model,
		http:    &http.Client{Timeout: 120 * time.Second},
	}
}

// Available probes the server's /api/tags endpoint and reports whether a
// working Ollama instance answers at the configured URL.
func (c *Client) Available(ctx context.Context) bool {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/api/tags", nil)
	if err != nil {
		return false
	}
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

func (c *Client) chat(ctx context.Context, prompt string) (string, error) {
	payload := map[string]any{
		"model":   c.model,
		"prompt":  prompt,
		"stream":  false,
		"options": map[string]any{"temperature": 0.3},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("encode request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/generate", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("ollama request: %w", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return "", fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("ollama %s: %s", resp.Status, strings.TrimSpace(string(data)))
	}

	var out struct {
		Response string `json:"response"`
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return "", fmt.Errorf("parse response: %w", err)
	}
	return strings.TrimSpace(out.Response), nil
}
