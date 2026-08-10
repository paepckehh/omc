// Package config reads omc's entire configuration from the environment.
//
// omc deliberately takes no command line arguments. Every knob is an
// environment variable so the tool stays composable in scripts and shells.
package config

import (
	"os"
	"strings"
)

// Config is the full runtime configuration derived from the environment.
type Config struct {
	// KeyPath points at an SSH private key used to sign the commit.
	// Optional: empty disables signing. When set but unusable, omc
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
	// Subject overrides the generated commit subject. When set, no LLM
	// generation runs. Pairing rules with Message are documented in
	// AGENTS.md and README.md.
	Subject string
	// Message overrides the generated commit body. When set, no LLM
	// generation runs. Pairing rules with Subject are documented in
	// AGENTS.md and README.md.
	Message string
	// Tag overrides the auto-bumped semver tag name. It is used verbatim
	// only when it parses as a strict vMAJOR.MINOR.PATCH semver tag;
	// otherwise the normal LatestSemverTag/NextSemverTag path runs.
	Tag string
	// PushKeyPath points at an SSH private key used to authenticate
	// against the repository's remote when pushing. Optional: empty
	// disables pushing. When set but unusable, omc logs a warning and
	// skips the push.
	PushKeyPath string
}

// FromEnv loads the configuration from the process environment.
func FromEnv() Config {
	return Config{
		KeyPath:     expandTilde(os.Getenv("OMC_SIGN_KEY_PATH")),
		OllamaURL:   os.Getenv("OLLAMA_DESC_URL"),
		OllamaModel: os.Getenv("OLLAMA_DESC_MODEL"),
		Name:        os.Getenv("OMC_NAME"),
		Email:       os.Getenv("OMC_EMAIL"),
		Subject:     os.Getenv("OMC_SUBJECT"),
		Message:     os.Getenv("OMC_MESSAGE"),
		Tag:         os.Getenv("OMC_TAG"),
		PushKeyPath: expandTilde(os.Getenv("OMC_PUSH_KEY_PATH")),
	}
}

// expandTilde rewrites a leading "~/" (or a bare "~") to the user's home
// directory. Go's os.ReadFile does not perform shell tilde expansion, so
// values quoted in shells or Makefiles (OMC_SIGN_KEY_PATH="~/.ssh/agent")
// would otherwise be passed through verbatim and fail to open. A path that
// is empty or does not start with "~" is returned unchanged.
func expandTilde(path string) string {
	if !strings.HasPrefix(path, "~") {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	if path == "~" {
		return home
	}
	if strings.HasPrefix(path, "~/") {
		return home + path[1:]
	}
	return path
}
