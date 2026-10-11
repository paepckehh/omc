package main

import (
	"strings"
	"testing"

	"paepcke.de/omc/internal/config"
	"paepcke.de/omc/internal/gitops"
)

// TestOneLine covers the startup-config value collapsing: multi-line values
// become one trimmed line and long values are capped with an ellipsis.
func TestOneLine(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"empty", "", ""},
		{"single", "hello", "hello"},
		{"multiline", "line one\nline two", "line one line two"},
		{"runs of whitespace", " a \t b\n\n c  ", "a b c"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := oneLine(tc.in); got != tc.want {
				t.Errorf("oneLine(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// TestOneLineCapsLength verifies the 120-char cap.
func TestOneLineCapsLength(t *testing.T) {
	in := strings.Repeat("x", 200)
	got := oneLine(in)
	if len(got) != 123 { // 120 + "..."
		t.Fatalf("len(oneLine) = %d, want 123", len(got))
	}
	if !strings.HasSuffix(got, "...") {
		t.Errorf("oneLine should end with ..., got %q", got)
	}
}

// TestCommitLabel covers the commit step label, signed and unsigned.
func TestCommitLabel(t *testing.T) {
	id := gitops.Identity{Name: "Ada", Email: "ada@example.com"}
	if got, want := commitLabel(id, true), "committing as Ada <ada@example.com> (signed)"; got != want {
		t.Errorf("commitLabel signed = %q, want %q", got, want)
	}
	if got, want := commitLabel(id, false), "committing as Ada <ada@example.com>"; got != want {
		t.Errorf("commitLabel unsigned = %q, want %q", got, want)
	}
}

// TestTagStepLabel covers the tag step label for a valid override, an
// invalid override, and no override at all.
func TestTagStepLabel(t *testing.T) {
	cases := []struct {
		name string
		cfg  config.Config
		want string
	}{
		{"no override", config.Config{}, "bumping semver patch"},
		{"valid override", config.Config{Tag: "v2.0.0"}, "tagging v2.0.0"},
		{"bare override", config.Config{Tag: "2.0.0"}, "tagging v2.0.0"},
		{"invalid override", config.Config{Tag: "not-semver"}, "bumping semver patch"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tagStepLabel(tc.cfg); got != tc.want {
				t.Errorf("tagStepLabel = %q, want %q", got, tc.want)
			}
		})
	}
}
