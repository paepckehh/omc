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
	for i := 0; i < n; i++ {
		b.WriteString("+line ")
		b.WriteString(itoa(i))
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
	for i := 0; i < 250; i++ {
		b.WriteString("+some line\n")
	}
	got := truncateDiff(b.String())
	if !strings.Contains(got, "diff truncated") {
		t.Fatalf("expected truncation marker for headerless oversized input")
	}
}

func TestDetailPrompt_ContainsTruncatedDiff(t *testing.T) {
	section := makeSection(300)
	prompt := DetailPrompt(section, "  - file.txt")
	if !strings.Contains(prompt, "diff truncated") {
		t.Fatalf("expected DetailPrompt to contain truncated diff")
	}
}

func TestItoa(t *testing.T) {
	cases := []int{0, 1, 9, 10, 250, -5}
	for _, n := range cases {
		want := strconv.Itoa(n)
		if got := itoa(n); got != want {
			t.Errorf("itoa(%d) = %q, want %q", n, got, want)
		}
	}
}

func firstN(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
