// Package config reads ocommit's entire configuration from the environment.
//
// ocommit deliberately takes no command line arguments. Every knob is an
// environment variable so the tool stays composable in scripts and shells.
package config

import "os"

// Config is the full runtime configuration derived from the environment.
type Config struct {
	// KeyPath points at an SSH private key used to sign the commit.
	// Optional: empty disables signing.
	KeyPath string
	// OllamaURL is the base URL of a local Ollama REST API, e.g.
	// http://127.0.0.1:11434. Optional: empty disables AI messages.
	OllamaURL string
	// OllamaModel selects the model hosted by Ollama. Optional; the
	// server's default model is used when empty.
	OllamaModel string
}

// FromEnv loads the configuration from the process environment.
func FromEnv() Config {
	return Config{
		KeyPath:     os.Getenv("OCOMMIT_KEY_PATH"),
		OllamaURL:   os.Getenv("OLLAMA_DESC_URL"),
		OllamaModel: os.Getenv("OLLAMA_DESC_MODEL"),
	}
}
