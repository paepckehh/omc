package config

import (
	"os"
	"strings"
	"testing"
)

func TestFromEnv(t *testing.T) {
	t.Setenv("OMC_SIGN_KEY_PATH", "/keys/id_ed25519")
	t.Setenv("OLLAMA_DESC_URL", "http://127.0.0.1:11434")
	t.Setenv("OLLAMA_DESC_MODEL", "qwen2.5:7b")
	t.Setenv("OMC_NAME", "Jane Doe")
	t.Setenv("OMC_EMAIL", "jane@example.com")
	t.Setenv("OMC_PUSH_KEY_PATH", "/keys/id_push")

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
	if c.PushKeyPath != "/keys/id_push" {
		t.Errorf("PushKeyPath = %q, want /keys/id_push", c.PushKeyPath)
	}
}

func TestFromEnvEmpty(t *testing.T) {
	t.Setenv("OMC_SIGN_KEY_PATH", "")
	t.Setenv("OLLAMA_DESC_URL", "")
	t.Setenv("OLLAMA_DESC_MODEL", "")
	t.Setenv("OMC_NAME", "")
	t.Setenv("OMC_EMAIL", "")
	t.Setenv("OMC_PUSH_KEY_PATH", "")

	c := FromEnv()
	if c.KeyPath != "" || c.OllamaURL != "" || c.OllamaModel != "" || c.Name != "" || c.Email != "" || c.PushKeyPath != "" {
		t.Errorf("expected all empty, got %+v", c)
	}
}

func TestFromEnvExpandsTilde(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("UserHomeDir: %v", err)
	}

	cases := []struct {
		in, want string
	}{
		{"~/.ssh/agent", home + "/.ssh/agent"},
		{"~/.ssh/", home + "/.ssh/"},
		{"~", home},
		{"", ""},
		{"/abs/key", "/abs/key"},
		{"relative/key", "relative/key"},
		{"~user/key", "~user/key"}, // ~user is not expanded
	}
	for _, tc := range cases {
		t.Run("path="+tc.in, func(t *testing.T) {
			got := expandTilde(tc.in)
			if got != tc.want {
				t.Errorf("expandTilde(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestFromEnvKeyPathTildeExpanded(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("UserHomeDir: %v", err)
	}
	t.Setenv("OMC_SIGN_KEY_PATH", "~/.ssh/agent")
	c := FromEnv()
	want := home + "/.ssh/agent"
	if c.KeyPath != want {
		t.Errorf("KeyPath = %q, want %q", c.KeyPath, want)
	}
	if !strings.HasPrefix(c.KeyPath, "/") {
		t.Errorf("KeyPath %q should be absolute after expansion", c.KeyPath)
	}
}

func TestFromEnvOverrides(t *testing.T) {
	t.Setenv("OMC_SUBJECT", "feat: add overrides")
	t.Setenv("OMC_MESSAGE", "detailed body\nspanning lines")
	t.Setenv("OMC_TAG", "v1.2.3")

	c := FromEnv()
	if c.Subject != "feat: add overrides" {
		t.Errorf("Subject = %q, want feat: add overrides", c.Subject)
	}
	if c.Message != "detailed body\nspanning lines" {
		t.Errorf("Message = %q, want the detailed body", c.Message)
	}
	if c.Tag != "v1.2.3" {
		t.Errorf("Tag = %q, want v1.2.3", c.Tag)
	}
}

func TestFromEnvOverridesEmpty(t *testing.T) {
	t.Setenv("OMC_SUBJECT", "")
	t.Setenv("OMC_MESSAGE", "")
	t.Setenv("OMC_TAG", "")

	c := FromEnv()
	if c.Subject != "" || c.Message != "" || c.Tag != "" {
		t.Errorf("expected empty overrides, got %+v", c)
	}
}
