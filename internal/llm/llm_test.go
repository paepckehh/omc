package llm

import (
	"strconv"
	"strings"
	"testing"
)

// makeSection builds a synthetic per-file diff section with n content lines
// (plus the "diff --git" header and a hunk header).
func makeSection(n int) string {
	var b strings.Builder
	b.WriteString("diff --git a/file.txt b/file.txt\n")
	b.WriteString("--- a/file.txt\n")
	b.WriteString("+++ b/file.txt\n")
	b.WriteString("@@ -1,n +1,n @@\n")
	for i := range n {
		b.WriteString("+line ")
		b.WriteString(strconv.Itoa(i))
		b.WriteByte('\n')
	}
	return b.String()
}

func TestTruncateDiff_PassesShortSectionsThrough(t *testing.T) {
	section := makeSection(50)
	got := truncateDiff(section)
	if got != section {
		t.Fatalf("expected short section unchanged, got difference")
	}
}

func TestTruncateDiff_CutsOversizedSection(t *testing.T) {
	// 250 content lines + 4 header lines = 254 section lines (>200).
	section := makeSection(250)
	got := truncateDiff(section)
	lines := strings.Split(got, "\n")

	// Expect truncation marker.
	if !strings.Contains(got, "diff truncated") {
		t.Fatalf("expected truncation marker, got:\n%s", got)
	}
	// The result must be shorter than the original.
	if len(lines) >= 254 {
		t.Fatalf("expected truncated output shorter than 254 lines, got %d", len(lines))
	}
	// Expect <= 100 original lines + 3 marker lines (blank, marker, blank).
	if len(lines) > 103 {
		t.Fatalf("expected at most ~103 lines, got %d", len(lines))
	}
}

func TestTruncateDiff_KeepsHeaderAndTail(t *testing.T) {
	section := makeSection(250)
	got := truncateDiff(section)
	// First line must be the diff header.
	if !strings.HasPrefix(got, "diff --git a/file.txt b/file.txt") {
		t.Fatalf("expected section to start with diff header, got start:\n%s", firstN(got, 80))
	}
	// Must keep a tail line from the original.
	if !strings.Contains(got, "+line 249") {
		t.Fatalf("expected tail content '+line 249' to survive truncation")
	}
}

func TestTruncateDiff_MultipleSections(t *testing.T) {
	short := makeSection(30)
	long := makeSection(300)
	diff := short + long

	got := truncateDiff(diff)
	// Short section must survive intact.
	if !strings.Contains(got, "+line 0\n+line 1\n+line 2") {
		t.Fatalf("expected short section content at top, got:\n%s", firstN(got, 300))
	}
	// Long section must be truncated.
	if !strings.Contains(got, "diff truncated") {
		t.Fatalf("expected truncation marker for long section")
	}
	// Tail of long section must survive.
	if !strings.Contains(got, "+line 299") {
		t.Fatalf("expected tail '+line 299' from long section")
	}
}

func TestTruncateDiff_Empty(t *testing.T) {
	if got := truncateDiff(""); got != "" {
		t.Fatalf("expected empty string back, got %q", got)
	}
}

func TestTruncateDiff_NoHeaders(t *testing.T) {
	// No "diff --git" lines → whole text treated as one section.
	var b strings.Builder
	for range 250 {
		b.WriteString("+some line\n")
	}
	got := truncateDiff(b.String())
	if !strings.Contains(got, "diff truncated") {
		t.Fatalf("expected truncation marker for headerless oversized input")
	}
}

func TestSanitizePassesCleanTextThrough(t *testing.T) {
	clean := "- fix the login race\n- tighten the token check\n\nBody text stays."
	if got := Sanitize(clean); got != clean {
		t.Fatalf("clean text must pass through unchanged, got:\n%q", got)
	}
}

func TestSanitizeStripsThinkBlocks(t *testing.T) {
	in := "<think>Let me look at the diff.\n</think>\n- fix the login race\n- tighten the token check"
	want := "- fix the login race\n- tighten the token check"
	if got := Sanitize(in); got != want {
		t.Fatalf("thinking block not stripped, got:\n%q", got)
	}
}

func TestSanitizeStripsMultipleThinkBlocks(t *testing.T) {
	in := "<think>reasoning one</think>first<think>reasoning two</think>second"
	want := "firstsecond"
	if got := Sanitize(in); got != want {
		t.Fatalf("multiple thinking blocks not stripped, got:\n%q", got)
	}
}

func TestSanitizeStripsUnclosedThinkBlock(t *testing.T) {
	// An opener without a closer: everything after it is reasoning.
	in := "- fix the login race\n<think>The rest is private chain-of-thought..."
	want := "- fix the login race"
	if got := Sanitize(in); got != want {
		t.Fatalf("unclosed thinking block not stripped, got:\n%q", got)
	}
}

func TestSanitizeStripsOtherThinkingTagPairs(t *testing.T) {
	cases := map[string]string{
		"<thinking>secret</thinking>visible":     "visible",
		"<|thinking|>secret<|/thinking|>visible": "visible",
		"<|think|>secret<|/think|>visible":       "visible",
		"<reasoning>secret</reasoning>visible":   "visible",
	}
	for in, want := range cases {
		if got := Sanitize(in); got != want {
			t.Errorf("Sanitize(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSanitizeStripsPromptEchoLines(t *testing.T) {
	in := "You are a git commit message assistant.\n" +
		"Write a detailed commit message explaining what changed.\n" +
		"DIFF STAT:\n" +
		"  - main.go\n" +
		"DIFF:\n" +
		"- fix the login race\n" +
		"- * Write a detailed commit message never mind\n" +
		"Use plain markdown bullets. Be specific.\n" +
		"Summarize the following git commit message into one TL;DR line\n"
	want := "- main.go\n- fix the login race"
	if got := Sanitize(in); got != want {
		t.Fatalf("prompt echo lines not stripped, got:\n%q", got)
	}
}

func TestSanitizeStripsLeadingTLDRLabel(t *testing.T) {
	cases := map[string]string{
		"TL;DR: fix the login race": "fix the login race",
		"TL;DR fix the login race":  "fix the login race",
		"tl;dr: fix the login race": "fix the login race",
		"TLDR: fix the login race":  "fix the login race",
		"TLDR fix the login race":   "fix the login race",
		"fix the login race":        "fix the login race",
	}
	for in, want := range cases {
		if got := Sanitize(in); got != want {
			t.Errorf("Sanitize(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSanitizeEmptyAndPureReasoning(t *testing.T) {
	if got := Sanitize(""); got != "" {
		t.Fatalf("empty input must stay empty, got %q", got)
	}
	if got := Sanitize("  \n  "); got != "" {
		t.Fatalf("blank input must sanitize to empty, got %q", got)
	}
	if got := Sanitize("<think>all reasoning, no answer"); got != "" {
		t.Fatalf("pure reasoning must sanitize to empty, got %q", got)
	}
	if got := Sanitize("You are a git commit message assistant.\nDIFF:\ndiff --git noise"); got != "diff --git noise" {
		t.Fatalf("prompt echo without reasoning must strip markers only, got %q", got)
	}
}

func TestSanitizeCombinedLeak(t *testing.T) {
	// A reasoning model that thinks, then echoes the prompt scaffolding
	// around its answer: only the answer survives.
	in := "<think>\nDiff shows a login change.\n</think>\n" +
		"DIFF STAT:\n" +
		"DIFF:\n" +
		"- fix the login race\n" +
		"- tighten the token check"
	want := "- fix the login race\n- tighten the token check"
	if got := Sanitize(in); got != want {
		t.Fatalf("combined leak not sanitized, got:\n%q", got)
	}
}

func TestDetailPrompt_ContainsTruncatedDiff(t *testing.T) {
	section := makeSection(300)
	prompt := DetailPrompt(section, "  - file.txt")
	if !strings.Contains(prompt, "diff truncated") {
		t.Fatalf("expected DetailPrompt to contain truncated diff")
	}
}

func firstN(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
