package output

import (
	"bytes"
	"errors"
	"strings"
	"testing"
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

func TestProgressNonTTY(t *testing.T) {
	ui, _, err := newTestUI()
	ui.Progress("condensing to TL;DR", 0.5)
	got := err.String()
	if !strings.Contains(got, "INFO") {
		t.Errorf("missing INFO level, got: %q", got)
	}
	if !strings.Contains(got, "ollama") {
		t.Errorf("missing ollama step, got: %q", got)
	}
	if !strings.Contains(got, "condensing to TL;DR") {
		t.Errorf("missing caption, got: %q", got)
	}
	if !strings.Contains(got, "50%") {
		t.Errorf("missing percentage, got: %q", got)
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
