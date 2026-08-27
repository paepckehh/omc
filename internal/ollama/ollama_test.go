package ollama

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
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := New(srv.URL, "")
	if !c.Available(context.Background()) {
		t.Fatal("expected available=true for reachable server")
	}
}

func TestAvailableUnreachable(t *testing.T) {
	c := New("http://127.0.0.1:1", "")
	if c.Available(context.Background()) {
		t.Fatal("expected available=false for unreachable server")
	}
}

func TestDescribeDetail(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/generate" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"response":"feat: add frobnicator"}`))
	}))
	defer srv.Close()

	c := New(srv.URL, "test-model")
	out, err := c.DescribeDetail(context.Background(), "diff...", "stats...")
	if err != nil {
		t.Fatal(err)
	}
	if out != "feat: add frobnicator" {
		t.Errorf("got %q", out)
	}
}

// TestDescribeDetailDistinctDiffAndStat guards against a regression where the
// diff text was passed as both the diff and the diffStat argument, duplicating
// the full diff in the prompt. The prompt must carry the diffStat separately
// from the diff.
func TestDescribeDetailDistinctDiffAndStat(t *testing.T) {
	const diffBody = "diff --git a/x.go b/x.go\n+hello"
	const statBody = "  - x.go"
	var gotPrompt string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req struct {
			Prompt string `json:"prompt"`
		}
		_ = json.Unmarshal(body, &req)
		gotPrompt = req.Prompt
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"response":"ok"}`))
	}))
	defer srv.Close()

	c := New(srv.URL, "test-model")
	if _, err := c.DescribeDetail(context.Background(), diffBody, statBody); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(gotPrompt, "DIFF STAT:\n"+statBody) {
		t.Errorf("prompt missing DIFF STAT section with stat content:\n%s", gotPrompt)
	}
	if !strings.Contains(gotPrompt, "DIFF:\n"+diffBody) {
		t.Errorf("prompt missing DIFF section with diff content:\n%s", gotPrompt)
	}
	// The stat line must appear exactly once (in the DIFF STAT section); the
	// full diff must not be duplicated into the DIFF STAT slot.
	if got := strings.Count(gotPrompt, statBody); got != 1 {
		t.Errorf("stat content appears %d times, want 1; prompt:\n%s", got, gotPrompt)
	}
}

func TestSummarizeTLDR(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"response":"Add frobnicator"}`))
	}))
	defer srv.Close()

	c := New(srv.URL, "test-model")
	out, err := c.SummarizeTLDR(context.Background(), "long detail...")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "frobnicator") {
		t.Errorf("got %q", out)
	}
}

func TestClientNewDefaultModel(t *testing.T) {
	c := New("http://x", "")
	if c.model != DefaultModel {
		t.Errorf("model = %q, want %q", c.model, DefaultModel)
	}
}
