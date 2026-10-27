package output

import (
	"bytes"
	"errors"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// newTestUI builds a UI whose stderr is a non-file buffer, so IsTTY() is
// false and the plain-text fallback path is exercised. This is the contract
// the e2e binary tests rely on: structured, greppable lines.
func newTestUI() (*UI, *bytes.Buffer, *bytes.Buffer) {
	out := &bytes.Buffer{}
	err := &bytes.Buffer{}
	return New(out, err), out, err
}

// newTestTTYUI builds a UI forced into TTY mode over plain buffers, so the
// animated spinner / rewrite paths can be exercised deterministically in
// tests. lipgloss is disabled so the emitted records are plain text and
// easy to assert on.
func newTestTTYUI() (*UI, *bytes.Buffer, *bytes.Buffer) {
	out := &bytes.Buffer{}
	err := &bytes.Buffer{}
	ui := New(out, err)
	ui.tty = true
	return ui, out, err
}
func TestStepSuccessNonTTY(t *testing.T) {
	ui, _, err := newTestUI()
	if err := ui.Step("stage", "staging all changes", func() error { return nil }); err != nil {
		t.Fatalf("Step returned error: %v", err)
	}
	got := err.String()
	if !strings.Contains(got, "INFO") {
		t.Errorf("missing INFO level on announcement, got: %q", got)
	}
	if !strings.Contains(got, "stage") {
		t.Errorf("missing step name, got: %q", got)
	}
	if !strings.Contains(got, "staging all changes") {
		t.Errorf("missing announcement message, got: %q", got)
	}
	if !strings.Contains(got, "OK") {
		t.Errorf("missing OK level on completion, got: %q", got)
	}
}

func TestStepErrorNonTTY(t *testing.T) {
	ui, _, err := newTestUI()
	want := errors.New("boom")
	if err := ui.Step("diff", "reading staged diff", func() error { return want }); err != want {
		t.Fatalf("Step returned %v, want %v", err, want)
	}
	got := err.String()
	// The announcement line must contain the step and the message.
	if !strings.Contains(got, "INFO") {
		t.Errorf("missing INFO level, got: %q", got)
	}
	if !strings.Contains(got, "diff") {
		t.Errorf("missing step name, got: %q", got)
	}
	if !strings.Contains(got, "reading staged diff") {
		t.Errorf("missing announcement message, got: %q", got)
	}
	// The failure line must contain the FAIL level and the error.
	if !strings.Contains(got, "FAIL") {
		t.Errorf("missing FAIL level, got: %q", got)
	}
	if !strings.Contains(got, "err=boom") {
		t.Errorf("missing structured err= field, got: %q", got)
	}
}

func TestWarnNonTTY(t *testing.T) {
	ui, _, err := newTestUI()
	ui.Warn("ssh key %s unusable (%v); committing unsigned", "/x", errors.New("nope"))
	got := err.String()
	if !strings.Contains(got, "WARN") {
		t.Errorf("missing WARN level, got: %q", got)
	}
	if !strings.Contains(got, "warning:") {
		t.Errorf("missing warning: marker, got: %q", got)
	}
	if !strings.Contains(got, "ssh key /x unusable") {
		t.Errorf("missing warning body, got: %q", got)
	}
}

func TestFieldsQuotedWhenNeeded(t *testing.T) {
	ui, _, err := newTestUI()
	ui.emit(ui.Err, "WARN", "", "boom", F("err", "agent refused"), F("url", "https://x/?a=b"), F("key", "/home/me/.ssh/agent"))
	got := err.String()
	for _, want := range []string{
		`err="agent refused"`,
		"url=https://x/?a=b",
		"key=/home/me/.ssh/agent",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("field %q not rendered correctly, got: %q", want, got)
		}
	}
}

func TestConfigNoticeNonTTY(t *testing.T) {
	ui, _, err := newTestUI()
	ui.ConfigNotice(
		[]Field{F("OMC_NAME", "Test"), F("OMC_TAG", "v1.2.3")},
		[]Field{F("sign_key", "valid=false"), F("tag_override", "valid=true")},
	)
	got := err.String()
	for _, wantSub := range []string{
		"INFO", "config", "detected environment", "count=2",
		"OMC_NAME=Test", "OMC_TAG=v1.2.3",
		"verified config", "sign_key=valid=false", "tag_override=valid=true",
	} {
		if !strings.Contains(got, wantSub) {
			t.Errorf("ConfigNotice output missing %q, got: %q", wantSub, got)
		}
	}
}

func TestCleanTreeNonTTY(t *testing.T) {
	ui, _, err := newTestUI()
	ui.CleanTree()
	got := err.String()
	if !strings.Contains(got, "OK") {
		t.Errorf("missing OK level, got: %q", got)
	}
	if !strings.Contains(got, "nothing to commit, working tree clean") {
		t.Errorf("missing clean-tree notice, got: %q", got)
	}
}

func TestCommitResultNonTTY(t *testing.T) {
	ui, out, _ := newTestUI()
	ui.CommitResult("9d3f2ab", true)
	got := out.String()
	if !strings.Contains(got, "OK") {
		t.Errorf("missing OK level, got: %q", got)
	}
	if !strings.Contains(got, "committed") {
		t.Errorf("missing committed message, got: %q", got)
	}
	if !strings.Contains(got, "hash=9d3f2ab") {
		t.Errorf("missing structured hash= field, got: %q", got)
	}
	if !strings.Contains(got, "signed=true") {
		t.Errorf("missing structured signed= field, got: %q", got)
	}
}

func TestCommitResultUnsignedNonTTY(t *testing.T) {
	ui, out, _ := newTestUI()
	ui.CommitResult("abc1234", false)
	got := out.String()
	if !strings.Contains(got, "signed=false") {
		t.Errorf("missing signed=false field, got: %q", got)
	}
}

func TestSpinnerNonTTY(t *testing.T) {
	ui, _, err := newTestUI()
	ui.SpinnerStart("ollama", "generating commit message")
	ui.SpinnerUpdate("condensing to TL;DR")
	ui.SpinnerStop()
	got := err.String()
	if !strings.Contains(got, "INFO") {
		t.Errorf("missing INFO level, got: %q", got)
	}
	if !strings.Contains(got, "ollama") {
		t.Errorf("missing ollama step, got: %q", got)
	}
	if !strings.Contains(got, "generating commit message") {
		t.Errorf("missing start caption, got: %q", got)
	}
	if !strings.Contains(got, "condensing to TL;DR") {
		t.Errorf("missing update caption, got: %q", got)
	}
	for _, line := range strings.Split(got, "\n") {
		if strings.Contains(line, "condensing to TL;DR") && !strings.Contains(line, "ollama") {
			t.Errorf("update phase lost its step name, got: %q", line)
		}
	}
}

func TestStartupDoesNotRepeatBrand(t *testing.T) {
	ui, _, err := newTestUI()
	ui.Startup("v9.9.9")
	got := err.String()
	if strings.Count(got, "omc") != 1 {
		t.Errorf("startup banner should name omc once, got: %q", got)
	}
	if !strings.Contains(got, "v9.9.9") {
		t.Errorf("startup banner missing version, got: %q", got)
	}
}

func TestFileListNonTTY(t *testing.T) {
	ui, _, err := newTestUI()
	ui.FileList([]string{"a.go", "b.go"})
	got := err.String()
	if !strings.Contains(got, "changed files") {
		t.Errorf("missing header, got: %q", got)
	}
	if !strings.Contains(got, "  - a.go") || !strings.Contains(got, "  - b.go") {
		t.Errorf("missing file items, got: %q", got)
	}
}

func TestFileListEmpty(t *testing.T) {
	ui, _, err := newTestUI()
	ui.FileList(nil)
	if err.Len() != 0 {
		t.Errorf("expected no output for empty file list, got: %q", err.String())
	}
}

func TestSummaryNonTTY(t *testing.T) {
	t.Run("with body", func(t *testing.T) {
		ui, out, _ := newTestUI()
		ui.Summary("fix: harden login", "details here")
		got := out.String()
		if !strings.Contains(got, "subject: fix: harden login") {
			t.Errorf("missing subject, got: %q", got)
		}
		if !strings.Contains(got, "body:") {
			t.Errorf("missing body marker, got: %q", got)
		}
		if !strings.Contains(got, "details here") {
			t.Errorf("missing body content, got: %q", got)
		}
	})
	t.Run("body only", func(t *testing.T) {
		ui, out, _ := newTestUI()
		ui.Summary("update", "")
		got := out.String()
		if !strings.Contains(got, "subject: update") {
			t.Errorf("missing subject, got: %q", got)
		}
		if strings.Contains(got, "body:") {
			t.Errorf("unexpected body section, got: %q", got)
		}
	})
}

func TestSigningNoticeNonTTY(t *testing.T) {
	t.Run("enabled", func(t *testing.T) {
		ui, _, err := newTestUI()
		ui.SigningNotice("/home/me/.ssh/agent", true)
		got := err.String()
		if !strings.Contains(got, "INFO") {
			t.Errorf("missing INFO level, got: %q", got)
		}
		if !strings.Contains(got, "signing commit with ssh key") {
			t.Errorf("missing signing notice, got: %q", got)
		}
		if !strings.Contains(got, "key=/home/me/.ssh/agent") {
			t.Errorf("missing structured key= field, got: %q", got)
		}
	})
	t.Run("disabled", func(t *testing.T) {
		ui, _, err := newTestUI()
		ui.SigningNotice("", false)
		got := err.String()
		if !strings.Contains(got, "OK") {
			t.Errorf("missing OK level, got: %q", got)
		}
		if !strings.Contains(got, "committing unsigned") {
			t.Errorf("missing unsigned notice, got: %q", got)
		}
		if !strings.Contains(got, "signed=false") {
			t.Errorf("missing structured signed=false field, got: %q", got)
		}
	})
}

func TestErrorNonTTY(t *testing.T) {
	ui, _, err := newTestUI()
	ui.Error(errors.New("not a git repository"))
	got := err.String()
	if !strings.Contains(got, "FAIL") {
		t.Errorf("missing FAIL level, got: %q", got)
	}
	if !strings.Contains(got, "not a git repository") {
		t.Errorf("missing error message, got: %q", got)
	}
}

func TestTagResultNonTTY(t *testing.T) {
	t.Run("unsigned", func(t *testing.T) {
		ui, out, _ := newTestUI()
		ui.TagResult("v0.0.1", "abc1234", false)
		got := out.String()
		if !strings.Contains(got, "OK") {
			t.Errorf("missing OK level, got: %q", got)
		}
		if !strings.Contains(got, "tagged") {
			t.Errorf("missing tagged message, got: %q", got)
		}
		if !strings.Contains(got, "tag=v0.0.1") {
			t.Errorf("missing structured tag= field, got: %q", got)
		}
		if !strings.Contains(got, "hash=abc1234") {
			t.Errorf("missing structured hash= field, got: %q", got)
		}
		if !strings.Contains(got, "signed=false") {
			t.Errorf("missing signed=false field, got: %q", got)
		}
	})
	t.Run("signed", func(t *testing.T) {
		ui, out, _ := newTestUI()
		ui.TagResult("v0.0.2", "def5678", true)
		got := out.String()
		if !strings.Contains(got, "tag=v0.0.2") {
			t.Errorf("missing tag field, got: %q", got)
		}
		if !strings.Contains(got, "hash=def5678") {
			t.Errorf("missing hash field, got: %q", got)
		}
		if !strings.Contains(got, "signed=true") {
			t.Errorf("missing signed=true field, got: %q", got)
		}
	})
}

func TestTimestampPresent(t *testing.T) {
	ui, _, err := newTestUI()
	ui.Info("hello")
	got := err.String()
	// Every structured line must begin with a HH:MM:SS timestamp.
	if len(got) < 9 || got[2] != ':' || got[5] != ':' {
		t.Errorf("missing HH:MM:SS timestamp prefix, got: %q", got)
	}
}

func TestStateEmoji(t *testing.T) {
	cases := map[string]string{
		"OK":    "✅",
		"INFO":  "ℹ️",
		"WARN":  "⚠️",
		"FAIL":  "❌",
		"BOGUS": "",
	}
	for level, want := range cases {
		if got := StateEmoji(level); got != want {
			t.Errorf("StateEmoji(%q) = %q, want %q", level, got, want)
		}
	}
}

func TestActionEmoji(t *testing.T) {
	cases := map[string]string{
		"open":     "📂",
		"stage":    "📥",
		"diff":     "🔍",
		"ollama":   "🤖",
		"load key": "🔑",
		"commit":   "📝",
		"tag":      "🏷️",
		"sign":     "✍️",
		"touch":    "🔐",
		"msg":      "💬",
		"unknown":  "",
	}
	for step, want := range cases {
		if got := ActionEmoji(step); got != want {
			t.Errorf("ActionEmoji(%q) = %q, want %q", step, got, want)
		}
	}
}

func TestEmitDecoratesStateAndAction(t *testing.T) {
	ui, out, _ := newTestUI()
	ui.CommitResult("9d3f2ab", true)
	got := out.String()
	if !strings.Contains(got, "✅") {
		t.Errorf("missing ✅ state emoji on OK, got: %q", got)
	}
	if !strings.Contains(got, "📝 commit") {
		t.Errorf("missing 📝 action emoji on commit step, got: %q", got)
	}
	// Text tokens must remain intact for greppability.
	if !strings.Contains(got, "OK") || !strings.Contains(got, "commit") {
		t.Errorf("text tokens lost: %q", got)
	}
}

// stripANSI removes CSI escape sequences so assertions can match the visible
// text regardless of coloring.
func stripANSI(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		if i+1 < len(s) && s[i] == 0x1b && s[i+1] == '[' {
			// skip to the terminator letter
			j := i + 2
			for j < len(s) && !isCSITerminator(s[j]) {
				j++
			}
			i = j // loop's i++ advances past the terminator
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

func isCSITerminator(c byte) bool {
	return c >= 0x40 && c <= 0x7e
}

func TestBeginGroupNoOpOffTTY(t *testing.T) {
	ui, _, err := newTestUI()
	ui.BeginGroup("g", 2)
	ui.EndGroup()
	if err.Len() != 0 {
		t.Errorf("BeginGroup/EndGroup wrote to stderr off a TTY, got: %q", err.String())
	}
	if len(ui.groups) != 0 {
		t.Error("group marked active off a TTY")
	}
}

func TestBeginGroupNoOpForZeroLines(t *testing.T) {
	ui, _, err := newTestTTYUI()
	ui.BeginGroup("g", 0)
	if len(ui.groups) != 0 {
		t.Error("group active for zero lines")
	}
	if err.Len() != 0 {
		t.Errorf("zero-line group wrote output, got: %q", err.String())
	}
}

func TestGroupTreeConnectors(t *testing.T) {
	ui, _, err := newTestTTYUI()
	ui.BeginGroup("setup", 3)
	ui.Info("first")
	ui.Info("middle")
	ui.Info("last")
	got := stripANSI(err.String())
	// Header line printed once.
	if !strings.Contains(got, "setup") {
		t.Errorf("missing group header, got: %q", got)
	}
	// Tree connectors: first ┌─, middle ├─, last └─.
	if !strings.Contains(got, "┌─") {
		t.Errorf("missing first-line ┌─ connector, got: %q", got)
	}
	if !strings.Contains(got, "├─") {
		t.Errorf("missing middle-line ├─ connector, got: %q", got)
	}
	if !strings.Contains(got, "└─") {
		t.Errorf("missing last-line └─ connector, got: %q", got)
	}
	// Group must auto-close after the configured line count.
	if len(ui.groups) != 0 {
		t.Error("group still active after all lines emitted")
	}
}

func TestGroupTreeSingleLine(t *testing.T) {
	ui, _, err := newTestTTYUI()
	ui.BeginGroup("solo", 1)
	ui.Info("only")
	if len(ui.groups) != 0 {
		t.Error("group still active after its single line")
	}
	got := stripANSI(err.String())
	if !strings.Contains(got, "└─") {
		t.Errorf("single-line group must use └─, got: %q", got)
	}
}

func TestEndGroupClosesEarlyAndSeparates(t *testing.T) {
	ui, _, err := newTestTTYUI()
	ui.BeginGroup("g", 5)
	ui.Info("one")
	ui.EndGroup()
	if len(ui.groups) != 0 {
		t.Error("EndGroup did not clear the group stack")
	}
	got := err.String()
	// EndGroup prints a trailing blank separator line on a TTY.
	if !strings.HasSuffix(got, "\n\n") {
		t.Errorf("EndGroup must print a blank separator, got: %q", got)
	}
}

func TestGroupAutoClosesAtLineCount(t *testing.T) {
	// A group auto-closes once its configured line count is emitted; a
	// further emit must NOT carry a tree connector.
	ui, _, err := newTestTTYUI()
	ui.BeginGroup("g", 1)
	ui.Info("in")  // grouped → └─
	ui.Info("out") // after auto-close → flat, no tree connector
	got := stripANSI(err.String())
	if strings.Count(got, "└─") != 1 {
		t.Errorf("expected exactly one └─ (grouped line only), got: %q", got)
	}
	if len(ui.groups) != 0 {
		t.Error("group not auto-closed after line count reached")
	}
}

func TestNestedGroupsRenderDepthConnectors(t *testing.T) {
	// A nested group's records must carry one connector per open level,
	// and the deeply nested record must end with the innermost group's
	// connector (└─ when it is the child's last line). The parent's budget
	// covers every record in its subtree (child header lines do not
	// consume it).
	ui, _, err := newTestTTYUI()
	ui.BeginGroup("parent", 4) // child-one + child-last + parent-last + one closing record
	ui.BeginGroup("child", 2)
	ui.Info("child-one")
	ui.Info("child-last")
	ui.Info("parent-last")
	ui.Info("parent-close")
	got := stripANSI(err.String())
	// child-one: parent still has remaining>1 → parent ├─; child first → ┌─.
	if !strings.Contains(got, "┌─ ┌─") {
		t.Errorf("child-first line must use parent first + child first connectors, got: %q", got)
	}
	// child-last: parent still open (remaining>1) → parent ├─; child last → └─.
	if !strings.Contains(got, "├─ └─") {
		t.Errorf("child-last line must use parent middle + child last connectors, got: %q", got)
	}
	// parent-last: emitted after the child group closed, so only parent's
	// connector remains, still └─ only on the parent's own last record.
	if !strings.Contains(got, "└─") {
		t.Errorf("parent close must carry the parent's └─ connector, got: %q", got)
	}
	if len(ui.groups) != 0 {
		t.Error("groups not all closed after their budgets were consumed")
	}
}

func TestNestedGroupHeaderIndent(t *testing.T) {
	// Nested headers must be prefixed with the parent connector so the
	// whole tree reads like git log --graph.
	ui, _, err := newTestTTYUI()
	ui.BeginGroup("parent", 2)
	ui.BeginGroup("child", 1)
	ui.Info("in")
	ui.EndGroup() // child
	got := stripANSI(err.String())
	// The child header line must carry a parent connector (┌─) prefix.
	if !strings.Contains(got, "child") || !strings.Contains(got, "parent") {
		t.Errorf("nested header missing, got: %q", got)
	}
	// Headers are now timestamped structured lines: each header begins with
	// a HH:MM:SS timestamp. The parent header is the top-level form
	// (timestamp + 3 spaces + title); the child header carries the parent's
	// first-line connector (┌─) before its title.
	lines := strings.Split(got, "\n")
	if len(lines) < 2 {
		t.Fatalf("expected at least 2 header lines, got: %q", got)
	}
	// Both headers must start with an 8-char HH:MM:SS timestamp.
	for i := range 2 {
		if len(lines[i]) < 8 || lines[i][2] != ':' || lines[i][5] != ':' {
			t.Errorf("header line %d missing HH:MM:SS timestamp prefix, got: %q", i, lines[i])
		}
	}
	if !strings.Contains(lines[0], "parent") {
		t.Errorf("parent header missing title, got: %q", lines[0])
	}
	if !strings.Contains(lines[1], "┌─") || !strings.Contains(lines[1], "child") {
		t.Errorf("child header must carry parent connector and title, got: %q", lines[1])
	}
}

func TestRenderFrameContainsSubjectAndBody(t *testing.T) {
	ui, _, _ := newTestTTYUI()
	got := stripANSI(ui.renderFrame("fix: harden login", "sanitize inputs"))
	if !strings.Contains(got, "fix: harden login") {
		t.Errorf("frame missing subject, got: %q", got)
	}
	if !strings.Contains(got, "sanitize inputs") {
		t.Errorf("frame missing body, got: %q", got)
	}
	if !strings.Contains(got, "commit message") {
		t.Errorf("frame missing title, got: %q", got)
	}
	// Rounded-border box characters must be present.
	if !strings.Contains(got, "╭") || !strings.Contains(got, "╰") {
		t.Errorf("frame missing rounded border, got: %q", got)
	}
}

func TestRenderFrameSubjectOnly(t *testing.T) {
	ui, _, _ := newTestTTYUI()
	got := stripANSI(ui.renderFrame("update", ""))
	if !strings.Contains(got, "update") {
		t.Errorf("frame missing subject, got: %q", got)
	}
	if !strings.Contains(got, "╭") {
		t.Errorf("frame missing border, got: %q", got)
	}
}

// newTestTTYUIWidth builds a TTY UI with an explicit terminal width so the
// frame-width logic can be exercised deterministically.
func newTestTTYUIWidth(width int) (*UI, *bytes.Buffer, *bytes.Buffer) {
	out := &bytes.Buffer{}
	err := &bytes.Buffer{}
	ui := &UI{Out: out, Err: err, tty: true, width: width}
	ui.initStyles()
	return ui, out, err
}

func TestFrameWidthFallsBackTo80WhenUnknown(t *testing.T) {
	// width == 0 (terminal size could not be probed) → 80-cell fallback.
	ui, _, _ := newTestTTYUIWidth(0)
	if got := ui.frameWidth(); got != 80 {
		t.Errorf("frameWidth() = %d for unknown width, want 80", got)
	}
}

func TestFrameWidthCappedAt80(t *testing.T) {
	ui, _, _ := newTestTTYUIWidth(200)
	if got := ui.frameWidth(); got != 80 {
		t.Errorf("frameWidth() = %d for 200-cell terminal, want 80 (cap)", got)
	}
}

func TestFrameWidthFollowsRealTerminal(t *testing.T) {
	ui, _, _ := newTestTTYUIWidth(40)
	if got := ui.frameWidth(); got != 40 {
		t.Errorf("frameWidth() = %d for 40-cell terminal, want 40", got)
	}
}

func TestFrameWidthFloorsAt8(t *testing.T) {
	ui, _, _ := newTestTTYUIWidth(3)
	if got := ui.frameWidth(); got != 8 {
		t.Errorf("frameWidth() = %d for 3-cell terminal, want 8 (floor)", got)
	}
}

func TestFrameWidthBudget(t *testing.T) {
	if got := frameContentBudget(80); got != 76 {
		t.Errorf("frameContentBudget(80) = %d, want 76", got)
	}
	if got := frameContentBudget(40); got != 36 {
		t.Errorf("frameContentBudget(40) = %d, want 36", got)
	}
}

func TestRenderFrameShortMessageFillsWidth(t *testing.T) {
	// A short subject must NOT collapse the frame to the content's natural
	// width: the frame must span the target width (80 here).
	ui, _, _ := newTestTTYUIWidth(0)
	got := stripANSI(ui.renderFrame("update", ""))
	lines := strings.Split(got, "\n")
	var topBorder string
	for _, l := range lines {
		if strings.HasPrefix(l, "╭") {
			topBorder = l
			break
		}
	}
	if topBorder == "" {
		t.Fatalf("missing top border line, got: %q", got)
	}
	if w := lipglossWidth(topBorder); w != 80 {
		t.Errorf("frame width = %d for short message, want 80 (frame must size to terminal, not content): %q", w, topBorder)
	}
}

func TestRenderFrameWrapsLongBody(t *testing.T) {
	// A long body line must wrap inside the frame rather than stretching it
	// past the target width.
	ui, _, _ := newTestTTYUIWidth(40)
	longBody := "This is a very long body line that definitely exceeds the 40 cell terminal width and must wrap."
	got := stripANSI(ui.renderFrame("subj", longBody))
	lines := strings.Split(got, "\n")
	_ = lines
	for _, l := range lines {
		if w := lipglossWidth(l); w > 40 {
			t.Errorf("frame line exceeds terminal width: %d cells: %q", w, l)
		}
	}
	if !strings.Contains(got, longBody[:20]) {
		t.Errorf("frame missing body content, got: %q", got)
	}
}

// lipglossWidth returns the visible width of a string in cells, ignoring
// ANSI escape sequences. Used by the frame-width assertions above.
func lipglossWidth(s string) int {
	return lipgloss.Width(stripANSI(s))
}

func TestSummaryTTYUsesFrame(t *testing.T) {
	ui, out, _ := newTestTTYUI()
	ui.Summary("feat: cool thing", "the body detail")
	got := stripANSI(out.String())
	if !strings.Contains(got, "feat: cool thing") {
		t.Errorf("TTY Summary missing subject, got: %q", got)
	}
	if !strings.Contains(got, "the body detail") {
		t.Errorf("TTY Summary missing body, got: %q", got)
	}
	if !strings.Contains(got, "╭") {
		t.Errorf("TTY Summary must render a frame, got: %q", got)
	}
}

func TestSummaryNonTTYUnchanged(t *testing.T) {
	ui, out, _ := newTestUI()
	ui.Summary("fix: bug", "body text")
	got := out.String()
	if !strings.Contains(got, "subject: fix: bug") {
		t.Errorf("non-TTY Summary missing subject:, got: %q", got)
	}
	if !strings.Contains(got, "body:") || !strings.Contains(got, "body text") {
		t.Errorf("non-TTY Summary missing body section, got: %q", got)
	}
	if strings.Contains(got, "╭") {
		t.Errorf("non-TTY Summary must not render a frame, got: %q", got)
	}
}

func TestEmitGroupedKeepsTokens(t *testing.T) {
	ui, out, _ := newTestTTYUI()
	ui.BeginGroup("g", 1)
	ui.CommitResult("9d3f2ab", true)
	// CommitResult writes to stdout (u.Out), so read from `out`.
	got := stripANSI(out.String())
	// Even inside a group the greppable tokens must survive.
	for _, want := range []string{"OK", "omc", "commit", "committed", "hash=9d3f2ab", "signed=true"} {
		if !strings.Contains(got, want) {
			t.Errorf("grouped line missing %q, got: %q", want, got)
		}
	}
}

// TestSpinnerStopStartsNextRecordOnFreshLine is the regression test for the
// LLM generation spinner: the live spinner rewrites its line with \r and no
// trailing newline, so SpinnerStop must erase the frame AND terminate the
// line. Without the terminator the next log record (the commit-message
// preview emitted right after the AI message/TL;DR phases) is stitched
// directly onto the spinner's line instead of starting on its own.
func TestSpinnerStopStartsNextRecordOnFreshLine(t *testing.T) {
	ui, _, errw := newTestTTYUI()

	ui.SpinnerStart("ollama", "generating commit message")
	ui.SpinnerUpdate("condensing to TL;DR")
	ui.SpinnerStop()
	ui.Info("next structured record")

	got := errw.String()
	if !strings.Contains(got, "\r\033[K\n") {
		t.Fatalf("SpinnerStop must erase the frame and terminate the line, got: %q", got)
	}
	// The record emitted after the stop must begin at the start of a line:
	// everything between the stop's terminating newline and the record's
	// timestamp must be only the timestamp prefix, with no spinner frame
	// residue stitched in front of the record.
	idx := strings.Index(got, "next structured record")
	if idx < 0 {
		t.Fatalf("INFO record missing from output: %q", got)
	}
	stopIdx := strings.LastIndex(got[:idx], "\r\033[K\n")
	if stopIdx < 0 {
		t.Fatalf("no terminated spinner line before the record, got: %q", got)
	}
	between := got[stopIdx+len("\r\033[K\n") : idx]
	if !regexp.MustCompile(`^\d\d:\d\d:\d\d `).MatchString(between) {
		t.Fatalf("record after SpinnerStop is stitched to the spinner line, residue: %q", between)
	}
}

// TestSpinnerStopNoEmptyLines ensures the fresh-line terminator never
// produces stray empty lines in the structured log stream: a record must
// follow directly on the terminated line, not after a pile of blank lines.
func TestSpinnerStopNoEmptyLines(t *testing.T) {
	ui, _, errw := newTestTTYUI()
	ui.SpinnerStart("ollama", "generating commit message")
	ui.SpinnerStop()
	ui.Info("record after stop")
	got := errw.String()
	// Exactly one newline terminates the spinner line; the record follows
	// on the same logical line, so there must be no blank line between.
	if strings.Contains(got, "\r\033[K\n\n") {
		t.Fatalf("spinner stop introduced an empty line, got: %q", got)
	}
}

// TestSpinnerConcurrentWritersDoNotClobber exercises the race the pty capture
// exposed: while a spinner frame is live, unguarded writers to the shared
// terminal (Summary's stdout frame, FileList's stderr list, EndGroup's
// separator) could interleave with the \r-rewrite loop. With the frame
// ticker slowed down, this run must terminate promptly under -race and the
// concurrent writers must not garble each other.
func TestSpinnerConcurrentWritersDoNotClobber(t *testing.T) {
	ui, out, errw := newTestTTYUI()
	ui.mu.Lock()
	ui.animWait = 5 * time.Millisecond
	ui.mu.Unlock()

	done := make(chan struct{})
	go func() {
		defer close(done)
		ui.SpinnerStart("ollama", "generating commit message")
		ui.SpinnerUpdate("condensing to TL;DR")
		time.Sleep(5 * time.Millisecond)
		ui.Summary("subject", "")
		ui.FileList([]string{"a.go", "b.go"})
		ui.BeginGroup("g", 1)
		ui.Info("grouped record")
		ui.EndGroup()
		ui.SpinnerStop()
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("concurrent writers deadlocked against the live spinner")
	}

	gotOut := stripANSI(out.String())
	if !strings.Contains(gotOut, "subject") {
		t.Errorf("Summary frame lost to the spinner, got: %q", gotOut)
	}
	gotErr := stripANSI(errw.String())
	for _, want := range []string{"a.go", "b.go", "grouped record"} {
		if !strings.Contains(gotErr, want) {
			t.Errorf("concurrent write %q garbled, got: %q", want, gotErr)
		}
	}
}
