package config

import (
	"testing"
)

// clearOpenAPIEnv sets every OPENAPI-related env var to empty so each test
// starts from a clean slate. t.Setenv ensures the values are restored when
// the test ends, so tests are atomic.
func clearOpenAPIEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{
		"OPENAPI_URL",
		"OPENAPI_MODEL",
		"OPENAPI_TOKEN",
		"OPENAPI_DESC_URL",
		"OPENAPI_DESC_MODEL",
		"OPENAPI_DESC_TOKEN",
	} {
		t.Setenv(k, "")
	}
}

func TestOpenAPIShortForm(t *testing.T) {
	clearOpenAPIEnv(t)
	t.Setenv("OPENAPI_URL", "http://10.0.0.1:8080/v1")
	t.Setenv("OPENAPI_MODEL", "gpt-oss")
	t.Setenv("OPENAPI_TOKEN", "tok-abc")

	c := FromEnv()
	if c.OpenAPIURL != "http://10.0.0.1:8080/v1" {
		t.Errorf("OpenAPIURL = %q, want http://10.0.0.1:8080/v1", c.OpenAPIURL)
	}
	if c.OpenAPIModel != "gpt-oss" {
		t.Errorf("OpenAPIModel = %q, want gpt-oss", c.OpenAPIModel)
	}
	if c.OpenAPIToken != "tok-abc" {
		t.Errorf("OpenAPIToken = %q, want tok-abc", c.OpenAPIToken)
	}
}

func TestOpenAPILegacyAlias(t *testing.T) {
	clearOpenAPIEnv(t)
	t.Setenv("OPENAPI_DESC_URL", "http://10.0.0.1:8080/v1")
	t.Setenv("OPENAPI_DESC_MODEL", "gpt-oss")
	t.Setenv("OPENAPI_DESC_TOKEN", "tok-abc")

	c := FromEnv()
	if c.OpenAPIURL != "http://10.0.0.1:8080/v1" {
		t.Errorf("OpenAPIURL = %q, want http://10.0.0.1:8080/v1", c.OpenAPIURL)
	}
	if c.OpenAPIModel != "gpt-oss" {
		t.Errorf("OpenAPIModel = %q, want gpt-oss", c.OpenAPIModel)
	}
	if c.OpenAPIToken != "tok-abc" {
		t.Errorf("OpenAPIToken = %q, want tok-abc", c.OpenAPIToken)
	}
}

func TestOpenAPIShortFormPrecedence(t *testing.T) {
	clearOpenAPIEnv(t)
	t.Setenv("OPENAPI_URL", "http://short/v1")
	t.Setenv("OPENAPI_MODEL", "short-model")
	t.Setenv("OPENAPI_TOKEN", "short-tok")
	t.Setenv("OPENAPI_DESC_URL", "http://legacy/v1")
	t.Setenv("OPENAPI_DESC_MODEL", "legacy-model")
	t.Setenv("OPENAPI_DESC_TOKEN", "legacy-tok")

	c := FromEnv()
	if c.OpenAPIURL != "http://short/v1" {
		t.Errorf("OpenAPIURL = %q, want http://short/v1 (short form takes precedence)", c.OpenAPIURL)
	}
	if c.OpenAPIModel != "short-model" {
		t.Errorf("OpenAPIModel = %q, want short-model", c.OpenAPIModel)
	}
	if c.OpenAPIToken != "short-tok" {
		t.Errorf("OpenAPIToken = %q, want short-tok", c.OpenAPIToken)
	}
}

func TestOpenAPIEmptyWhenUnset(t *testing.T) {
	clearOpenAPIEnv(t)

	c := FromEnv()
	if c.OpenAPIURL != "" {
		t.Errorf("OpenAPIURL = %q, want empty", c.OpenAPIURL)
	}
	if c.OpenAPIModel != "" {
		t.Errorf("OpenAPIModel = %q, want empty", c.OpenAPIModel)
	}
	if c.OpenAPIToken != "" {
		t.Errorf("OpenAPIToken = %q, want empty", c.OpenAPIToken)
	}
}

func TestOpenAPIPartialShortForm(t *testing.T) {
	clearOpenAPIEnv(t)
	t.Setenv("OPENAPI_URL", "http://10.0.0.1:8080/v1")
	t.Setenv("OPENAPI_DESC_MODEL", "legacy-model")
	t.Setenv("OPENAPI_DESC_TOKEN", "legacy-tok")

	c := FromEnv()
	if c.OpenAPIURL != "http://10.0.0.1:8080/v1" {
		t.Errorf("OpenAPIURL = %q, want http://10.0.0.1:8080/v1", c.OpenAPIURL)
	}
	if c.OpenAPIModel != "legacy-model" {
		t.Errorf("OpenAPIModel = %q, want legacy-model (fallback to legacy alias)", c.OpenAPIModel)
	}
	if c.OpenAPIToken != "legacy-tok" {
		t.Errorf("OpenAPIToken = %q, want legacy-tok (fallback to legacy alias)", c.OpenAPIToken)
	}
}

func TestOpenAPIEmptyShortFallsBackToLegacy(t *testing.T) {
	clearOpenAPIEnv(t)
	t.Setenv("OPENAPI_URL", "")
	t.Setenv("OPENAPI_DESC_URL", "http://legacy/v1")

	c := FromEnv()
	if c.OpenAPIURL != "http://legacy/v1" {
		t.Errorf("OpenAPIURL = %q, want http://legacy/v1 (empty short form falls back to legacy)", c.OpenAPIURL)
	}
}

func TestEnvOrAllEmpty(t *testing.T) {
	t.Setenv("OMC_TEST_VAR_A", "")
	t.Setenv("OMC_TEST_VAR_B", "")

	got := envOr("OMC_TEST_VAR_A", "OMC_TEST_VAR_B")
	if got != "" {
		t.Errorf("envOr with all empty = %q, want empty", got)
	}
}

func TestEnvOrFirstWins(t *testing.T) {
	t.Setenv("OMC_TEST_FIRST", "alpha")
	t.Setenv("OMC_TEST_SECOND", "beta")

	got := envOr("OMC_TEST_FIRST", "OMC_TEST_SECOND")
	if got != "alpha" {
		t.Errorf("envOr = %q, want alpha (first non-empty wins)", got)
	}
}

func TestEnvOrFallback(t *testing.T) {
	t.Setenv("OMC_TEST_FALLBACK_FIRST", "")
	t.Setenv("OMC_TEST_FALLBACK_SECOND", "gamma")

	got := envOr("OMC_TEST_FALLBACK_FIRST", "OMC_TEST_FALLBACK_SECOND")
	if got != "gamma" {
		t.Errorf("envOr = %q, want gamma (fallback to second)", got)
	}
}
