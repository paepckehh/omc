package openai

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAvailable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/models" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := New(srv.URL, "", "")
	if !c.Available(context.Background()) {
		t.Fatal("expected available=true for reachable server")
	}
}

func TestAvailableUnreachable(t *testing.T) {
	c := New("http://127.0.0.1:1", "", "")
	if c.Available(context.Background()) {
		t.Fatal("expected available=false for unreachable server")
	}
}

func TestAvailableSendsBearerToken(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := New(srv.URL, "", "secret-token")
	if !c.Available(context.Background()) {
		t.Fatal("expected available=true")
	}
	if gotAuth != "Bearer secret-token" {
		t.Errorf("Authorization = %q, want %q", gotAuth, "Bearer secret-token")
	}
}

func TestDescribeDetail(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"choices":[{"message":{"content":"feat: add frobnicator"}}]}`))
	}))
	defer srv.Close()

	c := New(srv.URL, "test-model", "")
	out, err := c.DescribeDetail(context.Background(), "diff...", "stats...")
	if err != nil {
		t.Fatal(err)
	}
	if out != "feat: add frobnicator" {
		t.Errorf("got %q", out)
	}
}

// TestDescribeDetailDistinctDiffAndStat guards against a regression where the
// diff text was passed as both the diff and the diffStat argument. The prompt
// must carry the diffStat separately from the diff (shared with the Ollama
// backend via internal/llm).
func TestDescribeDetailDistinctDiffAndStat(t *testing.T) {
	const diffBody = "diff --git a/x.go b/x.go\n+hello"
	const statBody = "  - x.go"
	var gotPrompt string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req struct {
			Messages []struct {
				Content string `json:"content"`
			} `json:"messages"`
		}
		_ = json.Unmarshal(body, &req)
		if len(req.Messages) > 0 {
			gotPrompt = req.Messages[0].Content
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}]}`))
	}))
	defer srv.Close()

	c := New(srv.URL, "test-model", "")
	if _, err := c.DescribeDetail(context.Background(), diffBody, statBody); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(gotPrompt, "DIFF STAT:\n"+statBody) {
		t.Errorf("prompt missing DIFF STAT section with stat content:\n%s", gotPrompt)
	}
	if !strings.Contains(gotPrompt, "DIFF:\n"+diffBody) {
		t.Errorf("prompt missing DIFF section with diff content:\n%s", gotPrompt)
	}
	if got := strings.Count(gotPrompt, statBody); got != 1 {
		t.Errorf("stat content appears %d times, want 1; prompt:\n%s", got, gotPrompt)
	}
}

func TestSummarizeTLDR(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"choices":[{"message":{"content":"Add frobnicator"}}]}`))
	}))
	defer srv.Close()

	c := New(srv.URL, "test-model", "")
	out, err := c.SummarizeTLDR(context.Background(), "long detail...")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "frobnicator") {
		t.Errorf("got %q", out)
	}
}

func TestClientNewDefaultModel(t *testing.T) {
	c := New("http://x", "", "")
	if c.model != DefaultModel {
		t.Errorf("model = %q, want %q", c.model, DefaultModel)
	}
}

func TestChatSendsBearerToken(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}]}`))
	}))
	defer srv.Close()

	c := New(srv.URL, "m", "tok-123")
	if _, err := c.SummarizeTLDR(context.Background(), "detail"); err != nil {
		t.Fatal(err)
	}
	if gotAuth != "Bearer tok-123" {
		t.Errorf("Authorization = %q, want %q", gotAuth, "Bearer tok-123")
	}
}

func TestChatErrorStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error":"bad token"}`))
	}))
	defer srv.Close()

	c := New(srv.URL, "m", "bad")
	_, err := c.SummarizeTLDR(context.Background(), "detail")
	if err == nil {
		t.Fatal("expected error for 401 response")
	}
	if !strings.Contains(err.Error(), "401") {
		t.Errorf("error should mention status 401: %v", err)
	}
}

func TestChatNoChoices(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"choices":[]}`))
	}))
	defer srv.Close()

	c := New(srv.URL, "m", "")
	_, err := c.SummarizeTLDR(context.Background(), "detail")
	if err == nil {
		t.Fatal("expected error for empty choices")
	}
}

// TestPromptMatchesOllama ensures the OpenAI backend uses the exact same
// prompt text as the Ollama backend (via internal/llm) so commit messages are
// comparable across backends.
func TestPromptMatchesOllama(t *testing.T) {
	const diffBody = "diff --git a/x.go b/x.go\n+hello"
	const statBody = "  - x.go"
	var gotPrompt string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req struct {
			Messages []struct {
				Content string `json:"content"`
			} `json:"messages"`
		}
		_ = json.Unmarshal(body, &req)
		if len(req.Messages) > 0 {
			gotPrompt = req.Messages[0].Content
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}]}`))
	}))
	defer srv.Close()

	c := New(srv.URL, "m", "")
	if _, err := c.DescribeDetail(context.Background(), diffBody, statBody); err != nil {
		t.Fatal(err)
	}
	want := "You are a git commit message assistant.\n" +
		"Write a detailed commit message explaining what changed and why, based only on the diff below.\n" +
		"Use plain markdown bullets. Be specific. Do not mention this prompt.\n\n" +
		"DIFF STAT:\n" + statBody + "\n\nDIFF:\n" + diffBody
	if gotPrompt != want {
		t.Errorf("prompt mismatch:\ngot:  %q\nwant: %q", gotPrompt, want)
	}
}
