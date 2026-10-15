// Package config reads omc's entire configuration from the environment.
//
// omc deliberately takes no command line arguments. Every knob is an
// environment variable so the tool stays composable in scripts and shells.
package config

import (
	"os"
	"strconv"
	"strings"
	"time"
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
	// OpenAPIURL is the base URL of an OpenAI-compatible REST API
	// (e.g. http://host:port/v1). Used as a fallback when Ollama is
	// not configured or unreachable. Optional: empty disables it.
	OpenAPIURL string
	// OpenAPIModel selects the model exposed by the OpenAI-compatible
	// endpoint. Optional; the endpoint's default is used when empty.
	OpenAPIModel string
	// OpenAPIToken is the bearer token sent as Authorization to the
	// OpenAI-compatible endpoint. Optional; some local endpoints
	// accept a placeholder token.
	OpenAPIToken string
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
	// TouchTimeout bounds how long omc waits for a FIDO2 security-key
	// (yubikey) touch before degrading (OMC_TOUCH_TIMEOUT, seconds).
	// Every smartcard wait — commit signing, tag signing, the upstream
	// tag pull, the push — is bounded by it; zero means wait forever,
	// matching OpenSSH's own unbounded behavior.
	TouchTimeout time.Duration
	// TouchNotify selects desktop notifications for touch prompts
	// (OMC_TOUCH_NOTIFY): "auto" (default, detect a notification daemon
	// via D-Bus at runtime), "on" (force), or "off".
	TouchNotify string
}

// DefaultTouchTimeout is the default wait for a security-key touch
// (OMC_TOUCH_TIMEOUT overrides it). OpenSSH and libfido2 wait for the touch
// indefinitely; omc's "always degrade, never block" contract instead fails
// the signature after this budget and proceeds unsigned, so a forgotten
// yubikey cannot hang a run. A generous default: it is a prompt window, not
// a performance limit.
const DefaultTouchTimeout = 60 * time.Second

// FromEnv loads the configuration from the process environment.
//
// The OpenAI-compatible endpoint accepts two env-var naming conventions:
//   - OPENAPI_URL / OPENAPI_MODEL / OPENAPI_TOKEN (short, preferred)
//   - OPENAPI_DESC_URL / OPENAPI_DESC_MODEL / OPENAPI_DESC_TOKEN (legacy)
//
// The short form takes precedence; the legacy form is kept for backward
// compatibility.
func FromEnv() Config {
	return Config{
		KeyPath:      expandTilde(os.Getenv("OMC_SIGN_KEY_PATH")),
		OllamaURL:    os.Getenv("OLLAMA_DESC_URL"),
		OllamaModel:  os.Getenv("OLLAMA_DESC_MODEL"),
		OpenAPIURL:   envOr("OPENAPI_URL", "OPENAPI_DESC_URL"),
		OpenAPIModel: envOr("OPENAPI_MODEL", "OPENAPI_DESC_MODEL"),
		OpenAPIToken: envOr("OPENAPI_TOKEN", "OPENAPI_DESC_TOKEN"),
		Name:         os.Getenv("OMC_NAME"),
		Email:        os.Getenv("OMC_EMAIL"),
		Subject:      os.Getenv("OMC_SUBJECT"),
		Message:      os.Getenv("OMC_MESSAGE"),
		Tag:          os.Getenv("OMC_TAG"),
		PushKeyPath:  expandTilde(os.Getenv("OMC_PUSH_KEY_PATH")),
		TouchTimeout: envSeconds("OMC_TOUCH_TIMEOUT", DefaultTouchTimeout),
		TouchNotify:  os.Getenv("OMC_TOUCH_NOTIFY"),
	}
}

// envSeconds parses a whole-seconds environment variable, returning def
// when unset or unparsable and clipping negatives to zero (wait forever).
func envSeconds(name string, def time.Duration) time.Duration {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return def
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return def
	}
	if n < 0 {
		n = 0
	}
	return time.Duration(n) * time.Second
}

// envOr returns the value of the first environment variable that is set and
// non-empty, falling back to the remaining names in order. This lets omc
// accept both short (OPENAPI_URL) and legacy (OPENAPI_DESC_URL) names without
// forcing a migration.
func envOr(names ...string) string {
	for _, n := range names {
		if v := os.Getenv(n); v != "" {
			return v
		}
	}
	return ""
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
