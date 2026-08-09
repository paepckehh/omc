// Package output centralizes all user-facing formatting for ocommit.
//
// It renders a small, modern terminal UI built on charmbracelet bubbles +
// lipgloss: animated spinners while a step runs, a gradient progress bar for
// the two-stage LLM generation, and styled headers for the final commit
// result and git log. Diagnostics and live progress go to stderr; the final
// commit result and history go to stdout, preserving the Unix convention that
// stdout carries program output.
//
// When stderr is not a terminal (piped output, captured tests, CI logs) the
// fancy live rendering is suppressed and ocommit falls back to the plain
// "ocommit: ..." line format so the output stays greppable and deterministic.
package output

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/bubbles/progress"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/term"
)

// Brand is the application label shown in styled headers.
const Brand = "ocommit"

// palette holds the lipgloss colors used across the UI.
var palette = struct {
	brand    lipgloss.Color
	subject  lipgloss.Color
	success  lipgloss.Color
	warn     lipgloss.Color
	err      lipgloss.Color
	muted    lipgloss.Color
	accent   lipgloss.Color
	detail   lipgloss.Color
	hash     lipgloss.Color
	signed   lipgloss.Color
	spinner  lipgloss.Color
	progress lipgloss.Color
}{
	brand:    lipgloss.Color("#7D56F4"),
	subject:  lipgloss.Color("#EE6FF8"),
	success:  lipgloss.Color("#3FB950"),
	warn:     lipgloss.Color("#D29922"),
	err:      lipgloss.Color("#F85149"),
	muted:    lipgloss.Color("#6E7681"),
	accent:   lipgloss.Color("#58A6FF"),
	detail:   lipgloss.Color("#A371F7"),
	hash:     lipgloss.Color("#79C0FF"),
	signed:   lipgloss.Color("#56D364"),
	spinner:  lipgloss.Color("#EE6FF8"),
	progress: lipgloss.Color("#7D56F4"),
}

// UI holds the two output streams plus rendering state.
type UI struct {
	Out io.Writer // program output (final result, git log)
	Err io.Writer // diagnostics, progress, errors

	tty      bool
	styles   styles
	progress progress.Model

	mu      sync.Mutex
	spin    spinner.Model
	spinOn  bool
	spinCtx chan struct{}
}

type styles struct {
	brand    lipgloss.Style
	step     lipgloss.Style
	ok       lipgloss.Style
	warn     lipgloss.Style
	fail     lipgloss.Style
	muted    lipgloss.Style
	subject  lipgloss.Style
	body     lipgloss.Style
	hash     lipgloss.Style
	signed   lipgloss.Style
	meta     lipgloss.Style
	bullet   lipgloss.Style
	rule     lipgloss.Style
	logHead  lipgloss.Style
	logMeta  lipgloss.Style
	logSubj  lipgloss.Style
	logSign  lipgloss.Style
	fileItem lipgloss.Style
	progress lipgloss.Style
}

// New returns a UI writing to the passed streams. TTY detection is performed
// on stderr: when stderr is a terminal the styled, animated UI is used;
// otherwise the plain text fallback is used.
func New(out, err io.Writer) *UI {
	tty := false
	if f, ok := err.(*os.File); ok {
		tty = term.IsTerminal(uintptr(f.Fd()))
	}
	ui := &UI{
		Out: out,
		Err: err,
		tty: tty,
	}
	ui.initStyles()
	ui.progress = progress.New(
		progress.WithGradient("#5A56E0", "#EE6FF8"),
		progress.WithoutPercentage(),
	)
	ui.progress.Width = 36
	ui.spin = spinner.New(spinner.WithSpinner(spinner.Dot), spinner.WithStyle(lipgloss.NewStyle().Foreground(palette.spinner)))
	return ui
}

func (u *UI) initStyles() {
	base := lipgloss.NewStyle()
	u.styles = styles{
		brand:    base.Bold(true).Foreground(palette.brand).Padding(0, 1, 0, 0),
		step:     base.Foreground(palette.muted),
		ok:       base.Bold(true).Foreground(palette.success),
		warn:     base.Bold(true).Foreground(palette.warn),
		fail:     base.Bold(true).Foreground(palette.err),
		muted:    base.Foreground(palette.muted),
		subject:  base.Bold(true).Foreground(palette.subject),
		body:     base.Foreground(palette.detail),
		hash:     base.Foreground(palette.hash).Bold(true),
		signed:   base.Foreground(palette.signed).Bold(true),
		meta:     base.Foreground(palette.muted),
		bullet:   base.Foreground(palette.accent),
		rule:     base.Foreground(palette.muted),
		logHead:  base.Foreground(palette.hash).Bold(true),
		logMeta:  base.Foreground(palette.muted),
		logSubj:  base.Foreground(palette.subject),
		logSign:  base.Foreground(palette.signed),
		fileItem: base.Foreground(palette.accent),
		progress: base.Foreground(palette.muted),
	}
}

// IsTTY reports whether the UI is rendering to an interactive terminal.
func (u *UI) IsTTY() bool { return u.tty }

// --- Step runner with spinner -------------------------------------------------

// Step announces and runs a single pipeline step. While fn runs an animated
// spinner labelled with step is shown on stderr; when fn returns the spinner
// is replaced by a completion line (green check on success, red cross and the
// error on failure). On a non-TTY it prints the plain "ocommit: ..." lines.
// It returns fn's error so the caller can decide whether to abort.
func (u *UI) Step(step, msg string, fn func() error) error {
	if !u.tty {
		u.plain(step, msg)
		err := fn()
		if err != nil {
			u.plainErr(step, err)
		}
		return err
	}

	u.startSpinner(step, msg)
	err := fn()
	u.stopSpinner()

	if err != nil {
		u.println(u.Err, u.failLine(step, err.Error()))
		return err
	}
	u.println(u.Err, u.okLine(step))
	return nil
}

// startSpinner launches a goroutine that animates the bubbles spinner on
// stderr. Only one spinner may be active at a time.
func (u *UI) startSpinner(step, msg string) {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.spinOn {
		return
	}
	u.spinOn = true
	u.spinCtx = make(chan struct{})

	label := u.styles.brand.Render(Brand)
	stepLbl := u.styles.step.Render(step)
	msgLbl := u.styles.muted.Render(msg)
	fps := u.spin.Spinner.FPS
	if fps <= 0 {
		fps = time.Second / 10 //nolint:mnd
	}

	go func(stop <-chan struct{}, stepLbl, msgLbl string) {
		tk := time.NewTicker(fps)
		defer tk.Stop()
		for {
			select {
			case <-stop:
				return
			case <-tk.C:
				u.mu.Lock()
				if !u.spinOn {
					u.mu.Unlock()
					return
				}
				u.spin, _ = u.spin.Update(spinner.TickMsg{Time: time.Now(), ID: u.spin.ID()})
				frame := u.spin.View()
				line := fmt.Sprintf("\r%s %s %s %s", label, frame, stepLbl, msgLbl)
				// pad to clear trailing chars from a previous, longer frame.
				fmt.Fprint(u.Err, line)
				u.mu.Unlock()
			}
		}
	}(u.spinCtx, stepLbl, msgLbl)
}

// stopSpinner stops the active spinner goroutine and clears its line.
func (u *UI) stopSpinner() {
	u.mu.Lock()
	if !u.spinOn {
		u.mu.Unlock()
		return
	}
	u.spinOn = false
	close(u.spinCtx)
	u.spinCtx = nil
	// Clear the spinner line.
	fmt.Fprint(u.Err, "\r\033[K")
	u.mu.Unlock()
}

// okLine renders a completed step line: "ocommit  ✓ step".
func (u *UI) okLine(step string) string {
	return fmt.Sprintf("%s %s %s",
		u.styles.brand.Render(Brand),
		u.styles.ok.Render("✓"),
		u.styles.step.Render(step),
	)
}

// failLine renders a failed step line: "ocommit  ✗ step: err".
func (u *UI) failLine(step, msg string) string {
	return fmt.Sprintf("%s %s %s %s",
		u.styles.brand.Render(Brand),
		u.styles.fail.Render("✗"),
		u.styles.step.Render(step),
		u.styles.fail.Render(msg),
	)
}

// --- Progress -----------------------------------------------------------------

// Progress renders a static progress bar at the given ratio (0..1) with an
// optional caption. On a non-TTY it prints a plain status line instead.
func (u *UI) Progress(caption string, ratio float64) {
	if ratio < 0 {
		ratio = 0
	}
	if ratio > 1 {
		ratio = 1
	}
	if !u.tty {
		u.plain("ollama", fmt.Sprintf("%s %3.0f%%", caption, ratio*100))
		return
	}
	bar := u.progress.ViewAs(ratio)
	cap := u.styles.progress.Render(caption)
	u.println(u.Err, fmt.Sprintf("%s %s %s", u.styles.brand.Render(Brand), cap, bar))
}

// --- Diagnostic / status lines ------------------------------------------------

// Warn prints a warning line (yellow). The literal word "warning" is kept so
// downstream consumers (and tests) can still grep for it.
func (u *UI) Warn(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	if !u.tty {
		fmt.Fprintf(u.Err, "%s: warning: %s\n", Brand, msg)
		return
	}
	u.println(u.Err, fmt.Sprintf("%s %s %s",
		u.styles.brand.Render(Brand),
		u.styles.warn.Render("⚠"),
		u.styles.warn.Render(msg),
	))
}

// Info prints a neutral diagnostic line.
func (u *UI) Info(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	if !u.tty {
		fmt.Fprintf(u.Err, "%s: %s\n", Brand, msg)
		return
	}
	u.println(u.Err, fmt.Sprintf("%s %s",
		u.styles.brand.Render(Brand),
		u.styles.muted.Render(msg),
	))
}

// Infof is retained for backward compatibility; it is an alias for Info.
func (u *UI) Infof(format string, args ...any) { u.Info(format, args...) }

// plain prints the non-TTY step announcement line.
func (u *UI) plain(step, msg string) {
	fmt.Fprintf(u.Err, "%s: %s %s\n", Brand, step, msg)
}

// plainErr prints the non-TTY step error line.
func (u *UI) plainErr(step string, err error) {
	fmt.Fprintf(u.Err, "%s: %s: %v\n", Brand, step, err)
}

// Printf prints program output to stdout (unchanged from the original API).
func (u *UI) Printf(format string, args ...any) {
	fmt.Fprintf(u.Out, format, args...)
}

// println writes a line terminated string to w.
func (u *UI) println(w io.Writer, s string) {
	fmt.Fprintln(w, s)
}

// --- Final result rendering ---------------------------------------------------

// CommitResult renders the final "committed" success block to stdout. hash is
// the short commit hash; the git log text is appended underneath. On a non-TTY
// it prints the original plain format so existing consumers and tests keep
// working (the substrings "committed" and "update" remain present).
func (u *UI) CommitResult(hash, logOut string) {
	if !u.tty {
		u.Printf("%s: committed %s\n%s", Brand, hash, logOut)
		return
	}

	header := lipgloss.JoinHorizontal(
		lipgloss.Left,
		u.styles.brand.Render(Brand),
		u.styles.ok.Render("✓"),
		u.styles.muted.Render("committed"),
		u.styles.hash.Render(hash),
	)
	body := strings.TrimRight(logOut, "\n")
	block := lipgloss.JoinVertical(lipgloss.Left, header, body)
	u.Println(block)
}

// TagResult renders the post-commit semver tag notice to stdout. signed
// indicates whether the tag was SSH-signed. On a non-TTY it prints a plain
// greppable line.
func (u *UI) TagResult(name, shortHash string, signed bool) {
	if !u.tty {
		if signed {
			fmt.Fprintf(u.Out, "%s: tagged %s %s (signed)\n", Brand, name, shortHash)
			return
		}
		fmt.Fprintf(u.Out, "%s: tagged %s %s\n", Brand, name, shortHash)
		return
	}
	var parts []string
	parts = append(parts,
		u.styles.brand.Render(Brand),
		u.styles.ok.Render("✓"),
		u.styles.muted.Render("tagged"),
		u.styles.subject.Render(name),
	)
	if signed {
		parts = append(parts, u.styles.signed.Render("(signed)"))
	}
	u.Println(lipgloss.JoinHorizontal(lipgloss.Left, parts...))
}

// CleanTree renders the "nothing to commit" notice to stderr.
func (u *UI) CleanTree() {
	if !u.tty {
		fmt.Fprintf(u.Err, "%s: nothing to commit, working tree clean\n", Brand)
		return
	}
	u.println(u.Err, fmt.Sprintf("%s %s %s",
		u.styles.brand.Render(Brand),
		u.styles.ok.Render("✓"),
		u.styles.muted.Render("nothing to commit, working tree clean"),
	))
}

// Error prints a fatal error to stderr and is used as the final failure
// report before exit.
func (u *UI) Error(err error) {
	if !u.tty {
		fmt.Fprintf(u.Err, "%s: %v\n", Brand, err)
		return
	}
	u.println(u.Err, fmt.Sprintf("%s %s %s",
		u.styles.brand.Render(Brand),
		u.styles.fail.Render("✗"),
		u.styles.fail.Render(err.Error()),
	))
}

// Println writes a pre-formatted line to stdout.
func (u *UI) Println(s string) {
	fmt.Fprintln(u.Out, s)
}

// FileList renders a compact bullet list of changed file paths under a header.
func (u *UI) FileList(files []string) {
	if len(files) == 0 {
		return
	}
	if !u.tty {
		fmt.Fprintf(u.Err, "%s: changed files:\n", Brand)
		for _, f := range files {
			fmt.Fprintf(u.Err, "  - %s\n", f)
		}
		return
	}
	header := fmt.Sprintf("%s %s",
		u.styles.brand.Render(Brand),
		u.styles.muted.Render("changed files:"),
	)
	var lines []string
	lines = append(lines, header)
	for _, f := range files {
		lines = append(lines, fmt.Sprintf("  %s %s", u.styles.bullet.Render("›"), u.styles.muted.Render(f)))
	}
	u.println(u.Err, lipgloss.JoinVertical(lipgloss.Left, lines...))
}

// Summary renders the resolved commit subject and (optional) LLM body to
// stdout as a styled preview before the commit is created.
func (u *UI) Summary(subject, body string) {
	if !u.tty {
		fmt.Fprintf(u.Out, "%s: subject: %s\n", Brand, subject)
		if strings.TrimSpace(body) != "" {
			fmt.Fprintf(u.Out, "%s: body:\n%s\n", Brand, body)
		}
		return
	}
	head := fmt.Sprintf("%s %s",
		u.styles.brand.Render(Brand),
		u.styles.muted.Render("commit message:"),
	)
	subj := u.styles.subject.Render(subject)
	block := []string{head, subj}
	if strings.TrimSpace(body) != "" {
		block = append(block, u.styles.body.Render(body))
	}
	u.Println(lipgloss.JoinVertical(lipgloss.Left, block...))
}

// SigningNotice renders the signing-on / signing-off notice line.
func (u *UI) SigningNotice(keyPath string, enabled bool) {
	if enabled {
		if !u.tty {
			fmt.Fprintf(u.Err, "%s: signing commit with ssh key %s\n", Brand, keyPath)
			return
		}
		u.println(u.Err, fmt.Sprintf("%s %s %s %s",
			u.styles.brand.Render(Brand),
			u.styles.signed.Render("🔑"),
			u.styles.muted.Render("signing with"),
			u.styles.meta.Render(keyPath),
		))
		return
	}
	if !u.tty {
		fmt.Fprintf(u.Err, "%s: committing unsigned\n", Brand)
		return
	}
	u.println(u.Err, fmt.Sprintf("%s %s %s",
		u.styles.brand.Render(Brand),
		u.styles.muted.Render("✓"),
		u.styles.muted.Render("committing unsigned"),
	))
}
