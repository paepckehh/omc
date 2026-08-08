package ollama

import (
	"context"
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

func TestDiffStat(t *testing.T) {
	stat := DiffStat(nil)
	if stat != "(no file changes)" {
		t.Errorf("DiffStat(nil) = %q", stat)
	}
}
