package output

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

// newTestUI builds a UI whose stderr is a non-file buffer, so IsTTY() is
// false and the plain-text fallback path is exercised. This is the contract
// the e2e binary tests rely on: greppable "ocommit: ..." lines.
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
	if !strings.Contains(got, "ocommit: stage staging all changes") {
		t.Errorf("missing announcement line, got: %q", got)
	}
}

func TestStepErrorNonTTY(t *testing.T) {
	ui, _, err := newTestUI()
	want := errors.New("boom")
	if err := ui.Step("diff", "reading staged diff", func() error { return want }); err != want {
		t.Fatalf("Step returned %v, want %v", err, want)
	}
	got := err.String()
	// The error line must contain the step, the announcement and the error.
	if !strings.Contains(got, "ocommit: diff reading staged diff") {
		t.Errorf("missing announcement line, got: %q", got)
	}
	if !strings.Contains(got, "ocommit: diff: boom") {
		t.Errorf("missing error line, got: %q", got)
	}
}

func TestWarnNonTTY(t *testing.T) {
	ui, _, err := newTestUI()
	ui.Warn("ssh key %s unusable (%v); committing unsigned", "/x", errors.New("nope"))
	got := err.String()
	if !strings.Contains(got, "ocommit: warning:") {
		t.Errorf("missing warning prefix, got: %q", got)
	}
	if !strings.Contains(got, "ssh key /x unusable") {
		t.Errorf("missing warning body, got: %q", got)
	}
}

func TestCleanTreeNonTTY(t *testing.T) {
	ui, _, err := newTestUI()
	ui.CleanTree()
	got := err.String()
	if !strings.Contains(got, "ocommit: nothing to commit, working tree clean") {
		t.Errorf("missing clean-tree notice, got: %q", got)
	}
}

func TestCommitResultNonTTY(t *testing.T) {
	ui, out, _ := newTestUI()
	ui.CommitResult("9d3f2ab", "9d3f2ab  A <a@x>  2026-08-08\n    update\n")
	got := out.String()
	if !strings.Contains(got, "ocommit: committed 9d3f2ab") {
		t.Errorf("missing committed header, got: %q", got)
	}
	if !strings.Contains(got, "update") {
		t.Errorf("missing log subject, got: %q", got)
	}
}

func TestProgressNonTTY(t *testing.T) {
	ui, _, err := newTestUI()
	ui.Progress("condensing to TL;DR", 0.5)
	got := err.String()
	if !strings.Contains(got, "ocommit: ollama condensing to TL;DR") {
		t.Errorf("missing progress line, got: %q", got)
	}
	if !strings.Contains(got, "50%") {
		t.Errorf("missing percentage, got: %q", got)
	}
}

func TestFileListNonTTY(t *testing.T) {
	ui, _, err := newTestUI()
	ui.FileList([]string{"a.go", "b.go"})
	got := err.String()
	if !strings.Contains(got, "ocommit: changed files:") {
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
		if !strings.Contains(got, "ocommit: subject: fix: harden login") {
			t.Errorf("missing subject, got: %q", got)
		}
		if !strings.Contains(got, "ocommit: body:\ndetails here") {
			t.Errorf("missing body, got: %q", got)
		}
	})
	t.Run("body only", func(t *testing.T) {
		ui, out, _ := newTestUI()
		ui.Summary("update", "")
		got := out.String()
		if !strings.Contains(got, "ocommit: subject: update") {
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
		if !strings.Contains(got, "ocommit: signing commit with ssh key /home/me/.ssh/agent") {
			t.Errorf("missing signing notice, got: %q", got)
		}
	})
	t.Run("disabled", func(t *testing.T) {
		ui, _, err := newTestUI()
		ui.SigningNotice("", false)
		got := err.String()
		if !strings.Contains(got, "ocommit: committing unsigned") {
			t.Errorf("missing unsigned notice, got: %q", got)
		}
	})
}

func TestErrorNonTTY(t *testing.T) {
	ui, _, err := newTestUI()
	ui.Error(errors.New("not a git repository"))
	got := err.String()
	if !strings.Contains(got, "ocommit: not a git repository") {
		t.Errorf("missing error line, got: %q", got)
	}
}

func TestIsTTYFalseForBuffer(t *testing.T) {
	ui, _, _ := newTestUI()
	if ui.IsTTY() {
		t.Error("IsTTY() = true for non-file writer, want false")
	}
}
