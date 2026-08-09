// Package config reads ocommit's entire configuration from the environment.
//
// ocommit deliberately takes no command line arguments. Every knob is an
// environment variable so the tool stays composable in scripts and shells.
package config

import (
	"os"
	"strings"
)

// Config is the full runtime configuration derived from the environment.
type Config struct {
	// KeyPath points at an SSH private key used to sign the commit.
	// Optional: empty disables signing. When set but unusable, ocommit
	// logs a warning and commits without signing.
	KeyPath string
	// OllamaURL is the base URL of a local Ollama REST API, e.g.
	// http://127.0.0.1:11434. Optional: empty disables AI messages.
	OllamaURL string
	// OllamaModel selects the model hosted by Ollama. Optional; the
	// server's default model is used when empty.
	OllamaModel string
	// Name is the commit author/committer name. Optional; falls back to
	// the repository's git config and then a built-in default.
	Name string
	// Email is the commit author/committer email. Same fallback chain
	// as Name.
	Email string
}

// FromEnv loads the configuration from the process environment.
func FromEnv() Config {
	return Config{
		KeyPath:     expandTilde(os.Getenv("OCOMMIT_KEY_PATH")),
		OllamaURL:   os.Getenv("OLLAMA_DESC_URL"),
		OllamaModel: os.Getenv("OLLAMA_DESC_MODEL"),
		Name:        os.Getenv("OCOMMIT_NAME"),
		Email:       os.Getenv("OCOMMIT_EMAIL"),
	}
}

// expandTilde rewrites a leading "~/" (or a bare "~") to the user's home
// directory. Go's os.ReadFile does not perform shell tilde expansion, so
// values quoted in shells or Makefiles (OCOMMIT_KEY_PATH="~/.ssh/agent")
// would otherwise be passed through verbatim and fail to open. A path that
// is empty or does not start with "~" is returned unchanged.
func expandTilde(path string) string {
	if path == "" {
		return path
	}
	if path == "~" {
		home, err := os.UserHomeDir()
		if err != nil {
			return path
		}
		return home
	}
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return path
		}
		return home + path[1:]
	}
	return path
}
