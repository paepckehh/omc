package config

import "testing"

func TestFromEnv(t *testing.T) {
	t.Setenv("OCOMMIT_KEY_PATH", "/keys/id_ed25519")
	t.Setenv("OLLAMA_DESC_URL", "http://127.0.0.1:11434")
	t.Setenv("OLLAMA_DESC_MODEL", "qwen2.5:7b")
	t.Setenv("OCOMMIT_NAME", "Jane Doe")
	t.Setenv("OCOMMIT_EMAIL", "jane@example.com")

	c := FromEnv()
	if c.KeyPath != "/keys/id_ed25519" {
		t.Errorf("KeyPath = %q, want /keys/id_ed25519", c.KeyPath)
	}
	if c.OllamaURL != "http://127.0.0.1:11434" {
		t.Errorf("OllamaURL = %q, want http://127.0.0.1:11434", c.OllamaURL)
	}
	if c.OllamaModel != "qwen2.5:7b" {
		t.Errorf("OllamaModel = %q, want qwen2.5:7b", c.OllamaModel)
	}
	if c.Name != "Jane Doe" {
		t.Errorf("Name = %q, want Jane Doe", c.Name)
	}
	if c.Email != "jane@example.com" {
		t.Errorf("Email = %q, want jane@example.com", c.Email)
	}
}

func TestFromEnvEmpty(t *testing.T) {
	t.Setenv("OCOMMIT_KEY_PATH", "")
	t.Setenv("OLLAMA_DESC_URL", "")
	t.Setenv("OLLAMA_DESC_MODEL", "")
	t.Setenv("OCOMMIT_NAME", "")
	t.Setenv("OCOMMIT_EMAIL", "")

	c := FromEnv()
	if c.KeyPath != "" || c.OllamaURL != "" || c.OllamaModel != "" || c.Name != "" || c.Email != "" {
		t.Errorf("expected all empty, got %+v", c)
	}
}
