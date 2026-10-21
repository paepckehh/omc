package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"unicode/utf8"

	"paepcke.de/omc/internal/config"
	"paepcke.de/omc/internal/llm"
	"paepcke.de/omc/internal/ollama"
	"paepcke.de/omc/internal/openai"
	"paepcke.de/omc/internal/output"
)

func TestResolveGeneratorsOllamaPreferred(t *testing.T) {
	cfg := config.Config{
		OllamaURL:   "http://127.0.0.1:11434",
		OllamaModel: "llama3.2",
		OpenAPIURL:  "http://10.0.0.1:8080/v1",
	}
	cands := resolveGenerators(cfg)
	if len(cands) != 2 {
		t.Fatalf("expected 2 candidates, got %d", len(cands))
	}
	if cands[0].step != "ollama" {
		t.Errorf("first candidate step = %q, want ollama", cands[0].step)
	}
	if cands[0].url != cfg.OllamaURL {
		t.Errorf("first candidate url = %q, want %q", cands[0].url, cfg.OllamaURL)
	}
	if _, ok := cands[0].gen.(*ollama.Client); !ok {
		t.Errorf("expected *ollama.Client, got %T", cands[0].gen)
	}
	if cands[1].step != "openai" {
		t.Errorf("second candidate step = %q, want openai", cands[1].step)
	}
	if _, ok := cands[1].gen.(*openai.Client); !ok {
		t.Errorf("expected *openai.Client, got %T", cands[1].gen)
	}
}

func TestResolveGeneratorsOpenAIFallback(t *testing.T) {
	cfg := config.Config{
		OpenAPIURL:   "http://10.0.0.1:8080/v1",
		OpenAPIModel: "gpt-oss",
		OpenAPIToken: "tok",
	}
	cands := resolveGenerators(cfg)
	if len(cands) != 1 {
		t.Fatalf("expected 1 candidate, got %d", len(cands))
	}
	if cands[0].step != "openai" {
		t.Errorf("step = %q, want openai", cands[0].step)
	}
	if cands[0].url != cfg.OpenAPIURL {
		t.Errorf("url = %q, want %q", cands[0].url, cfg.OpenAPIURL)
	}
	if _, ok := cands[0].gen.(*openai.Client); !ok {
		t.Errorf("expected *openai.Client, got %T", cands[0].gen)
	}
}

func TestResolveGeneratorsNone(t *testing.T) {
	cands := resolveGenerators(config.Config{})
	if len(cands) != 0 {
		t.Errorf("expected no candidates, got %d", len(cands))
	}
}

func TestResolveGeneratorsOllamaEmptyURLSkipsOllama(t *testing.T) {
	// Ollama model set but URL empty: Ollama is not usable, only OpenAI remains.
	cfg := config.Config{
		OllamaModel: "llama3.2",
		OpenAPIURL:  "http://10.0.0.1:8080/v1",
	}
	cands := resolveGenerators(cfg)
	if len(cands) != 1 {
		t.Fatalf("expected 1 candidate, got %d", len(cands))
	}
	if cands[0].step != "openai" {
		t.Errorf("step = %q, want openai (fallback)", cands[0].step)
	}
}

// TestGeneratorsImplementInterface is a compile-time check that both backend
// clients satisfy the llm.Generator contract.
func TestGeneratorsImplementInterface(t *testing.T) {
	var _ llm.Generator = (*ollama.Client)(nil)
	var _ llm.Generator = (*openai.Client)(nil)
}

func TestConfigReportMasksToken(t *testing.T) {
	cfg := config.Config{
		OpenAPIURL:   "http://10.0.0.1:8080/v1",
		OpenAPIModel: "gpt-oss",
		OpenAPIToken: "super-secret-token",
	}
	detected, verified := configReport(cfg)
	for _, f := range detected {
		if f.Key == "OPENAPI_TOKEN" {
			if strings.Contains(f.Val, "super-secret-token") {
				t.Errorf("token value leaked into detected report: %s", f.Val)
			}
			if f.Val != "(set)" {
				t.Errorf("token reported as %q, want (set)", f.Val)
			}
		}
	}
	var sawOpenAIVerified bool
	for _, f := range verified {
		if f.Key == "openai" {
			sawOpenAIVerified = true
		}
	}
	if !sawOpenAIVerified {
		t.Errorf("expected openai verified entry when OpenAPIURL set")
	}
}

func TestConfigReportNoTokenWhenEmpty(t *testing.T) {
	cfg := config.Config{
		OpenAPIURL: "http://10.0.0.1:8080/v1",
	}
	detected, _ := configReport(cfg)
	for _, f := range detected {
		if f.Key == "OPENAPI_TOKEN" {
			t.Errorf("did not expect OPENAPI_TOKEN in detected report when empty")
		}
	}
}

// stubGenerator is a scripted llm.Generator for exercising the fallback
// chain without any network.
type stubGenerator struct {
	available   bool
	detail      string
	detailErr   error
	tldr        string
	tldrErr     error
	detailCalls int
}

func (s *stubGenerator) Available(context.Context) bool { return s.available }

func (s *stubGenerator) DescribeDetail(context.Context, string, string) (string, error) {
	s.detailCalls++
	return s.detail, s.detailErr
}

func (s *stubGenerator) SummarizeTLDR(context.Context, string) (string, error) {
	return s.tldr, s.tldrErr
}

func newLLMTestUI() *output.UI {
	return output.New(&bytes.Buffer{}, &bytes.Buffer{})
}

func TestGenerateMessageFallsBackWhenUnreachable(t *testing.T) {
	first := &stubGenerator{available: false}
	second := &stubGenerator{available: true, detail: "body", tldr: "subject"}
	got := generateMessage(newLLMTestUI(), context.Background(), []llmCandidate{
		{gen: first, step: "ollama", url: "http://one"},
		{gen: second, step: "openai", url: "http://two"},
	}, "diff", nil)
	if got.Subject != "subject" || got.Body != "body" {
		t.Errorf("message = %+v, want subject/body from the second backend", got)
	}
	if first.detailCalls != 0 {
		t.Errorf("unreachable backend was asked to generate")
	}
}

func TestGenerateMessageFallsBackWhenDetailFails(t *testing.T) {
	first := &stubGenerator{available: true, detailErr: errors.New("boom")}
	second := &stubGenerator{available: true, detail: "body", tldr: "subject"}
	got := generateMessage(newLLMTestUI(), context.Background(), []llmCandidate{
		{gen: first, step: "ollama", url: "http://one"},
		{gen: second, step: "openai", url: "http://two"},
	}, "diff", nil)
	if got.Subject != "subject" || got.Body != "body" {
		t.Errorf("message = %+v, want the fallback backend's message", got)
	}
}

func TestGenerateMessageSalvagesDetailWhenTLDRFails(t *testing.T) {
	first := &stubGenerator{available: true, detail: "first line\nmore detail", tldrErr: errors.New("boom")}
	second := &stubGenerator{available: true, detail: "other", tldr: "other"}
	got := generateMessage(newLLMTestUI(), context.Background(), []llmCandidate{
		{gen: first, step: "ollama", url: "http://one"},
		{gen: second, step: "openai", url: "http://two"},
	}, "diff", nil)
	if got.Subject != "first line" || got.Body != "first line\nmore detail" {
		t.Errorf("message = %+v, want the salvaged detail", got)
	}
	if second.detailCalls != 0 {
		t.Errorf("fallback backend called despite a usable partial result")
	}
}

func TestGenerateMessageSanitizesSubject(t *testing.T) {
	long := strings.Repeat("x", 100) + "\nsecond line"
	gen := &stubGenerator{available: true, detail: "body", tldr: long}
	got := generateMessage(newLLMTestUI(), context.Background(), []llmCandidate{
		{gen: gen, step: "ollama", url: "http://one"},
	}, "diff", nil)
	if strings.ContainsRune(got.Subject, '\n') {
		t.Errorf("subject still contains a newline: %q", got.Subject)
	}
	if n := utf8.RuneCountInString(got.Subject); n > 72 {
		t.Errorf("subject = %d runes, want <= 72", n)
	}
}

func TestGenerateMessageAllBackendsFail(t *testing.T) {
	got := generateMessage(newLLMTestUI(), context.Background(), []llmCandidate{
		{gen: &stubGenerator{available: true, detailErr: errors.New("boom")}, step: "ollama", url: "http://one"},
	}, "diff", nil)
	if got.Subject != "update" {
		t.Errorf("subject = %q, want update", got.Subject)
	}
}
