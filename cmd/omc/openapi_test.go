package main

import (
	"strings"
	"testing"

	"paepcke.de/omc/internal/config"
	"paepcke.de/omc/internal/ollama"
	"paepcke.de/omc/internal/openai"
)

func TestResolveGeneratorsOllamaOnly(t *testing.T) {
	t.Parallel()
	cfg := config.Config{
		OllamaURL:   "http://127.0.0.1:11434",
		OllamaModel: "llama3.2",
	}
	cands := resolveGenerators(cfg)
	if len(cands) != 1 {
		t.Fatalf("expected 1 candidate, got %d", len(cands))
	}
	if cands[0].step != "ollama" {
		t.Errorf("step = %q, want ollama", cands[0].step)
	}
	if _, ok := cands[0].gen.(*ollama.Client); !ok {
		t.Errorf("expected *ollama.Client, got %T", cands[0].gen)
	}
}

func TestResolveGeneratorsOpenAIOnly(t *testing.T) {
	t.Parallel()
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

func TestResolveGeneratorsBoth(t *testing.T) {
	t.Parallel()
	cfg := config.Config{
		OllamaURL:  "http://127.0.0.1:11434",
		OpenAPIURL: "http://10.0.0.1:8080/v1",
	}
	cands := resolveGenerators(cfg)
	if len(cands) != 2 {
		t.Fatalf("expected 2 candidates, got %d", len(cands))
	}
	if cands[0].step != "ollama" {
		t.Errorf("first step = %q, want ollama", cands[0].step)
	}
	if cands[1].step != "openai" {
		t.Errorf("second step = %q, want openai", cands[1].step)
	}
}

func TestResolveGeneratorsEmpty(t *testing.T) {
	t.Parallel()
	cands := resolveGenerators(config.Config{})
	if len(cands) != 0 {
		t.Errorf("expected 0 candidates, got %d", len(cands))
	}
}

func TestResolveGeneratorsOllamaModelOnlySkipped(t *testing.T) {
	t.Parallel()
	cfg := config.Config{
		OllamaModel: "llama3.2",
		OpenAPIURL:  "http://10.0.0.1:8080/v1",
	}
	cands := resolveGenerators(cfg)
	if len(cands) != 1 {
		t.Fatalf("expected 1 candidate, got %d", len(cands))
	}
	if cands[0].step != "openai" {
		t.Errorf("step = %q, want openai (Ollama URL empty, only OpenAI)", cands[0].step)
	}
}

func TestConfigReportOpenAPIURLDetected(t *testing.T) {
	t.Parallel()
	cfg := config.Config{
		OpenAPIURL: "http://10.0.0.1:8080/v1",
	}
	detected, _ := configReport(cfg)
	var sawURL bool
	for _, f := range detected {
		if f.Key == "OPENAPI_URL" && f.Val == "http://10.0.0.1:8080/v1" {
			sawURL = true
		}
	}
	if !sawURL {
		t.Errorf("expected OPENAPI_URL in detected report")
	}
}

func TestConfigReportOpenAPIModelDetected(t *testing.T) {
	t.Parallel()
	cfg := config.Config{
		OpenAPIModel: "gpt-oss",
	}
	detected, _ := configReport(cfg)
	var sawModel bool
	for _, f := range detected {
		if f.Key == "OPENAPI_MODEL" && f.Val == "gpt-oss" {
			sawModel = true
		}
	}
	if !sawModel {
		t.Errorf("expected OPENAPI_MODEL in detected report")
	}
}

func TestConfigReportOpenAPITokenMasked(t *testing.T) {
	t.Parallel()
	cfg := config.Config{
		OpenAPIToken: "super-secret-token",
	}
	detected, _ := configReport(cfg)
	for _, f := range detected {
		if f.Key == "OPENAPI_TOKEN" {
			if strings.Contains(f.Val, "super-secret-token") {
				t.Errorf("token value leaked: %s", f.Val)
			}
			if f.Val != "(set)" {
				t.Errorf("token reported as %q, want (set)", f.Val)
			}
			return
		}
	}
	t.Errorf("expected OPENAPI_TOKEN in detected report when set")
}

func TestConfigReportOpenAIVerified(t *testing.T) {
	t.Parallel()
	cfg := config.Config{
		OpenAPIURL: "http://10.0.0.1:8080/v1",
	}
	_, verified := configReport(cfg)
	var sawVerified bool
	for _, f := range verified {
		if f.Key == "openai" && f.Val == "configured=true" {
			sawVerified = true
		}
	}
	if !sawVerified {
		t.Errorf("expected openai=configured=true in verified report")
	}
}

func TestConfigReportNoOpenAPIWhenEmpty(t *testing.T) {
	t.Parallel()
	cfg := config.Config{}
	detected, verified := configReport(cfg)
	for _, f := range detected {
		if strings.HasPrefix(f.Key, "OPENAPI") {
			t.Errorf("did not expect OPENAPI_* in detected report when empty, got %s=%s", f.Key, f.Val)
		}
	}
	for _, f := range verified {
		if f.Key == "openai" {
			t.Errorf("did not expect openai in verified report when OpenAPIURL empty")
		}
	}
}

func TestConfigReportOllamaVerified(t *testing.T) {
	t.Parallel()
	cfg := config.Config{
		OllamaURL: "http://127.0.0.1:11434",
	}
	_, verified := configReport(cfg)
	var sawVerified bool
	for _, f := range verified {
		if f.Key == "ollama" && f.Val == "configured=true" {
			sawVerified = true
		}
	}
	if !sawVerified {
		t.Errorf("expected ollama=configured=true in verified report")
	}
}

func TestConfigReportAllOpenAPIFields(t *testing.T) {
	t.Parallel()
	cfg := config.Config{
		OpenAPIURL:   "http://10.0.0.1:8080/v1",
		OpenAPIModel: "gpt-oss",
		OpenAPIToken: "secret",
	}
	detected, verified := configReport(cfg)

	var sawURL, sawModel, sawToken bool
	for _, f := range detected {
		switch f.Key {
		case "OPENAPI_URL":
			sawURL = true
		case "OPENAPI_MODEL":
			sawModel = true
		case "OPENAPI_TOKEN":
			sawToken = true
		}
	}
	if !sawURL {
		t.Errorf("missing OPENAPI_URL in detected")
	}
	if !sawModel {
		t.Errorf("missing OPENAPI_MODEL in detected")
	}
	if !sawToken {
		t.Errorf("missing OPENAPI_TOKEN in detected")
	}

	var sawVerified bool
	for _, f := range verified {
		if f.Key == "openai" {
			sawVerified = true
		}
	}
	if !sawVerified {
		t.Errorf("missing openai in verified")
	}
}
