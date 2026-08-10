package output

import (
	"bytes"
	"errors"
	"strings"
	"testing"

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

func TestSecurityKeyTouchNoticeNonTTY(t *testing.T) {
	ui, _, err := newTestUI()
	ui.SecurityKeyTouchNotice("/home/me/.ssh/id_ed25519_sk", "the commit signing")
	got := err.String()
	if !strings.Contains(got, "INFO") {
		t.Errorf("missing INFO level, got: %q", got)
	}
	if !strings.Contains(got, "touch") {
		t.Errorf("missing touch step, got: %q", got)
	}
	if !strings.Contains(got, "smartcard/yubikey") {
		t.Errorf("missing smartcard/yubikey prompt, got: %q", got)
	}
	if !strings.Contains(got, "the commit signing") {
		t.Errorf("missing action description, got: %q", got)
	}
	if !strings.Contains(got, "key=/home/me/.ssh/id_ed25519_sk") {
		t.Errorf("missing structured key= field, got: %q", got)
	}
	if !strings.Contains(got, "mode=smartcard") {
		t.Errorf("missing structured mode= field, got: %q", got)
	}
	if !strings.Contains(got, "action=the commit signing") {
		t.Errorf("missing structured action= field, got: %q", got)
	}
}

func TestStepTouchCommitNonTTY(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		ui, _, err := newTestUI()
		if eerr := ui.StepTouchCommit("/k/id_ed25519_sk", "the commit signing", "commit", "committing as Test <t@e> (signed)", func() error { return nil }); eerr != nil {
			t.Fatalf("StepTouchCommit returned error: %v", eerr)
		}
		got := err.String()
		if !strings.Contains(got, "INFO") || !strings.Contains(got, "commit") {
			t.Errorf("missing announce line, got: %q", got)
		}
		if !strings.Contains(got, "OK") || !strings.Contains(got, "done") {
			t.Errorf("missing done line, got: %q", got)
		}
		if !strings.Contains(got, "touch confirmed, thank you") {
			t.Errorf("missing touch-confirmed record, got: %q", got)
		}
	})
	t.Run("failure", func(t *testing.T) {
		ui, _, err := newTestUI()
		want := errors.New("agent unreachable")
		if eerr := ui.StepTouchCommit("/k/id_ed25519_sk", "the commit signing", "commit", "committing", func() error { return want }); eerr == nil {
			t.Fatal("StepTouchCommit returned nil for failing fn")
		}
		got := err.String()
		if !strings.Contains(got, "FAIL") || !strings.Contains(got, "agent unreachable") {
			t.Errorf("missing FAIL record, got: %q", got)
		}
	})
}

func TestStepTouchPushNonTTY(t *testing.T) {
	ui, _, err := newTestUI()
	if eerr := ui.StepTouchPush("/k/id_ed25519_sk", "the push", "push", "pushing commit and tags to remote", func() error { return nil }); eerr != nil {
		t.Fatalf("StepTouchPush returned error: %v", eerr)
	}
	got := err.String()
	if !strings.Contains(got, "push") || !strings.Contains(got, "OK") {
		t.Errorf("missing push step records, got: %q", got)
	}
	if !strings.Contains(got, "touch confirmed, thank you") {
		t.Errorf("missing touch-confirmed record, got: %q", got)
	}
}

func TestTouchCountdownRender(t *testing.T) {
	cd := newTouchCountdown()
	got := cd.render()
	if !strings.Contains(got, "TOUCH YOUR SECURITY KEY") {
		t.Errorf("missing touch prompt, got: %q", got)
	}
	if !strings.Contains(got, "⏱") {
		t.Errorf("missing timer glyph, got: %q", got)
	}
	if !strings.Contains(got, "0:30") {
		t.Errorf("missing initial 0:30 seconds, got: %q", got)
	}
	if !strings.Contains(got, "▱") {
		t.Errorf("missing empty progress bar cells, got: %q", got)
	}

	// Advance ~halfway: bar must contain both filled and empty cells and
	// the timer must read roughly 15s.
	cd.remaining = 15.0
	got = cd.render()
	if !strings.Contains(got, "0:15") {
		t.Errorf("missing 0:15 after advance, got: %q", got)
	}
	if !strings.Contains(got, "▰") || !strings.Contains(got, "▱") {
		t.Errorf("missing mixed progress bar cells, got: %q", got)
	}

	// Exhaust the countdown: timer clamps to 0:00, bar is fully filled.
	cd.remaining = 0
	got = cd.render()
	if !strings.Contains(got, "0:00") {
		t.Errorf("missing 0:00 at zero, got: %q", got)
	}
	if !strings.Contains(got, "▰") {
		t.Errorf("missing filled progress bar cells at zero, got: %q", got)
	}
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

func TestIsTTYFalseForBuffer(t *testing.T) {
	ui, _, _ := newTestUI()
	if ui.IsTTY() {
		t.Error("IsTTY() = true for non-file writer, want false")
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

// newTestTTYUI builds a UI that reports IsTTY() == true, so the styled,
// tree-grouped rendering paths are exercised. The writers are plain buffers,
// so the rendered strings carry no real ANSI cursor control but do carry
// lipgloss color escapes — the substring checks below stay robust to that.
func newTestTTYUI() (*UI, *bytes.Buffer, *bytes.Buffer) {
	out := &bytes.Buffer{}
	err := &bytes.Buffer{}
	ui := &UI{Out: out, Err: err, tty: true}
	ui.initStyles()
	return ui, out, err
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
	if !strings.HasPrefix(stripANSI(err.String()), "  parent\n┌─   child") {
		t.Errorf("child header must be prefixed with the parent's first-line connector, got: %q", got)
	}
}

func TestTouchConfirmedTextOnTTY(t *testing.T) {
	ui, _, err := newTestTTYUI()
	ui.TouchStart("/k/id_ed25519_sk", "the commit signing")
	ui.TouchStop()
	ui.TouchConfirmed()
	got := stripANSI(err.String())
	if !strings.Contains(got, "touch confirmed, thank you") {
		t.Errorf("TTY touch confirmation missing, got: %q", got)
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
