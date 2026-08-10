package main

import (
	"testing"

	"paepcke.de/omc/internal/config"
	"paepcke.de/omc/internal/gitops"
)

func TestOverrideMessageNone(t *testing.T) {
	msg, ok := overrideMessage(config.Config{})
	if ok {
		t.Errorf("expected no override when both empty, got %+v", msg)
	}
}

func TestOverrideMessageBoth(t *testing.T) {
	msg, ok := overrideMessage(config.Config{Subject: "subj", Message: "body"})
	if !ok {
		t.Fatal("expected override active")
	}
	if msg.Subject != "subj" {
		t.Errorf("Subject = %q, want subj", msg.Subject)
	}
	if msg.Body != "body" {
		t.Errorf("Body = %q, want body", msg.Body)
	}
}

func TestOverrideMessageSubjectOnly(t *testing.T) {
	msg, ok := overrideMessage(config.Config{Subject: "only subject"})
	if !ok {
		t.Fatal("expected override active")
	}
	if msg.Subject != "only subject" {
		t.Errorf("Subject = %q, want only subject", msg.Subject)
	}
	if msg.Body != "only subject" {
		t.Errorf("Body = %q, want only subject (same as subject)", msg.Body)
	}
}

func TestOverrideMessageMessageOnly(t *testing.T) {
	msg, ok := overrideMessage(config.Config{Message: "first line\nsecond line"})
	if !ok {
		t.Fatal("expected override active")
	}
	if msg.Subject != "first line" {
		t.Errorf("Subject = %q, want first line", msg.Subject)
	}
	if msg.Body != "first line\nsecond line" {
		t.Errorf("Body = %q, want full message", msg.Body)
	}
}

func TestOverrideMessageTrimsWhitespace(t *testing.T) {
	msg, ok := overrideMessage(config.Config{Subject: "  spaced  ", Message: "  body  "})
	if !ok {
		t.Fatal("expected override active")
	}
	if msg.Subject != "spaced" {
		t.Errorf("Subject = %q, want spaced", msg.Subject)
	}
	if msg.Body != "body" {
		t.Errorf("Body = %q, want body", msg.Body)
	}
}

func TestShortenSubject(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"first line\nsecond", "first line"},
		{"  \n  \nactual first\nrest", "actual first"},
		{"single line", "single line"},
		{"", ""},
		{long72Plus(), long72Exactly()},
	}
	for _, c := range cases {
		got := shortenSubject(c.in)
		if got != c.want {
			t.Errorf("shortenSubject(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func long72Plus() string    { return repeatChar('a', 80) }
func long72Exactly() string { return repeatChar('a', 72) }

func repeatChar(b byte, n int) string {
	out := make([]byte, n)
	for i := range out {
		out[i] = b
	}
	return string(out)
}

func TestResolveTagNameOverrideValid(t *testing.T) {
	cfg := config.Config{Tag: "v5.4.3"}
	got, _, err := resolveTagName(nil, cfg)
	if err != nil {
		t.Fatalf("resolveTagName: %v", err)
	}
	if got != "v5.4.3" {
		t.Errorf("got %q, want v5.4.3", got)
	}
}

func TestResolveTagNameOverrideBareGetsVPrefix(t *testing.T) {
	cfg := config.Config{Tag: "5.4.3"}
	got, _, err := resolveTagName(nil, cfg)
	if err != nil {
		t.Fatalf("resolveTagName: %v", err)
	}
	if got != "v5.4.3" {
		t.Errorf("got %q, want v5.4.3 (v added)", got)
	}
}

func TestResolveTagNameOverrideLargeSegments(t *testing.T) {
	cfg := config.Config{Tag: "v9999.888.7777"}
	got, _, err := resolveTagName(nil, cfg)
	if err != nil {
		t.Fatalf("resolveTagName: %v", err)
	}
	if got != "v9999.888.7777" {
		t.Errorf("got %q, want v9999.888.7777", got)
	}
}

// TestResolveTagNameInvalidOverrideFallsBack is a sanity check that an
// invalid override yields the empty-string latest-bump result without a
// repo (LatestSemverTag(nil,...) is not callable here, so we only assert
// ValidSemverTag rejects the value, which is the gate the pipeline uses).
func TestResolveTagNameInvalidOverrideRejected(t *testing.T) {
	if gitops.ValidSemverTag("v1.2") {
		t.Fatal("v1.2 must not be a valid strict semver tag")
	}
	if gitops.ValidSemverTag("v1.2.3-rc.1") {
		t.Fatal("pre-release suffix must be rejected")
	}
}
