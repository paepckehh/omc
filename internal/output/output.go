// Package output centralizes all user-facing formatting for ocommit.
//
// It renders a structured, timestamped terminal UI built on charmbracelet
// bubbles + lipgloss. Every line emitted — diagnostics, progress, and final
// results — is a structured log record of the form:
//
//	<HH:MM:SS> <LEVEL> ocommit [<step>] <message> [key=value ...]
//
// Examples (plain, non-TTY form):
//
//	12:04:07 INFO  ocommit open   detecting repository
//	12:04:07 OK    ocommit stage  done
//	12:04:08 WARN  ocommit        ssh key unusable  key=/x err="nope"
//	12:04:09 OK    ocommit commit committed     hash=9d3f2ab signed=true
//	12:04:09 OK    ocommit tag    tagged         tag=v0.0.3 hash=9d3f2ab signed=true
//
// When stderr is a terminal the same records are rendered with color, icons,
// aligned columns, and a gradient progress bar for the two-stage LLM
// generation. Diagnostics and live progress go to stderr; the final commit
// and tag results go to stdout, preserving the Unix convention that stdout
// carries program output.
//
// When stderr is not a terminal (piped output, captured tests, CI logs) the
// colored rendering is suppressed and ocommit falls back to the plain
// structured line format so the output stays greppable and deterministic.
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

// Brand is the application label shown in every structured log line.
const Brand = "ocommit"

// palette holds the lipgloss colors used across the UI.
var palette = struct {
	brand     lipgloss.Color
	subject   lipgloss.Color
	success   lipgloss.Color
	warn      lipgloss.Color
	err       lipgloss.Color
	muted     lipgloss.Color
	accent    lipgloss.Color
	detail    lipgloss.Color
	hash      lipgloss.Color
	signed    lipgloss.Color
	timestamp lipgloss.Color
	info      lipgloss.Color
	step      lipgloss.Color
	key       lipgloss.Color
	val       lipgloss.Color
	spinner   lipgloss.Color
	progress  lipgloss.Color
}{
	brand:     lipgloss.Color("#7D56F4"),
	subject:   lipgloss.Color("#EE6FF8"),
	success:   lipgloss.Color("#3FB950"),
	warn:      lipgloss.Color("#D29922"),
	err:       lipgloss.Color("#F85149"),
	muted:     lipgloss.Color("#6E7681"),
	accent:    lipgloss.Color("#58A6FF"),
	detail:    lipgloss.Color("#A371F7"),
	hash:      lipgloss.Color("#79C0FF"),
	signed:    lipgloss.Color("#56D364"),
	timestamp: lipgloss.Color("#6E7681"),
	info:      lipgloss.Color("#58A6FF"),
	step:      lipgloss.Color("#A371F7"),
	key:       lipgloss.Color("#6E7681"),
	val:       lipgloss.Color("#C9D1D9"),
	spinner:   lipgloss.Color("#EE6FF8"),
	progress:  lipgloss.Color("#7D56F4"),
}

// Field is a single structured key=value pair appended to a log line.
type Field struct {
	Key string
	Val string
}

// F is a shorthand constructor for a Field.
func F(k, v string) Field { return Field{Key: k, Val: v} }

// UI holds the two output streams plus rendering state.
type UI struct {
	Out io.Writer // program output (final result, tag result)
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
	brand     lipgloss.Style
	timestamp lipgloss.Style
	levelOK   lipgloss.Style
	levelInfo lipgloss.Style
	levelWarn lipgloss.Style
	levelFail lipgloss.Style
	step      lipgloss.Style
	msg       lipgloss.Style
	key       lipgloss.Style
	val       lipgloss.Style
	ok        lipgloss.Style
	warn      lipgloss.Style
	fail      lipgloss.Style
	muted     lipgloss.Style
	subject   lipgloss.Style
	body      lipgloss.Style
	hash      lipgloss.Style
	signed    lipgloss.Style
	meta      lipgloss.Style
	bullet    lipgloss.Style
	fileItem  lipgloss.Style
	progress  lipgloss.Style
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
		brand:     base.Bold(true).Foreground(palette.brand),
		timestamp: base.Foreground(palette.timestamp),
		levelOK:   base.Bold(true).Foreground(palette.success),
		levelInfo: base.Bold(true).Foreground(palette.info),
		levelWarn: base.Bold(true).Foreground(palette.warn),
		levelFail: base.Bold(true).Foreground(palette.err),
		step:      base.Foreground(palette.step).Bold(true),
		msg:       base.Foreground(palette.val),
		key:       base.Foreground(palette.key),
		val:       base.Foreground(palette.val),
		ok:        base.Bold(true).Foreground(palette.success),
		warn:      base.Bold(true).Foreground(palette.warn),
		fail:      base.Bold(true).Foreground(palette.err),
		muted:     base.Foreground(palette.muted),
		subject:   base.Bold(true).Foreground(palette.subject),
		body:      base.Foreground(palette.detail),
		hash:      base.Foreground(palette.hash).Bold(true),
		signed:    base.Foreground(palette.signed).Bold(true),
		meta:      base.Foreground(palette.muted),
		bullet:    base.Foreground(palette.accent),
		fileItem:  base.Foreground(palette.accent),
		progress:  base.Foreground(palette.muted),
	}
}

// IsTTY reports whether the UI is rendering to an interactive terminal.
func (u *UI) IsTTY() bool { return u.tty }

// --- structured log line rendering -------------------------------------------

// now returns the current time formatted as HH:MM:SS.
func now() string { return time.Now().Format("15:04:05") }

// renderFields renders a slice of key=value fields as "key=val key=val".
func (u *UI) renderFields(fields []Field) string {
	if len(fields) == 0 {
		return ""
	}
	var parts []string
	for _, f := range fields {
		if u.tty {
			parts = append(parts, fmt.Sprintf("%s=%s",
				u.styles.key.Render(f.Key),
				u.styles.val.Render(f.Val)))
		} else {
			parts = append(parts, fmt.Sprintf("%s=%s", f.Key, f.Val))
		}
	}
	return strings.Join(parts, " ")
}

// emit writes a structured log record to w. level is one of OK/INFO/WARN/FAIL.
// step is the pipeline step name (may be empty); msg is the human message;
// fields are optional key=value pairs. The record always carries a leading
// timestamp and the brand name so consumers can grep on "ocommit" regardless
// of TTY mode.
func (u *UI) emit(w io.Writer, level, step, msg string, fields ...Field) {
	ts := now()
	fieldStr := u.renderFields(fields)
	if !u.tty {
		var b strings.Builder
		b.WriteString(ts)
		b.WriteByte(' ')
		b.WriteString(level)
		b.WriteString(" ")
		b.WriteString(Brand)
		if step != "" {
			b.WriteByte(' ')
			b.WriteString(step)
		}
		b.WriteByte(' ')
		b.WriteString(msg)
		if fieldStr != "" {
			b.WriteByte(' ')
			b.WriteString(fieldStr)
		}
		b.WriteByte('\n')
		fmt.Fprint(w, b.String())
		return
	}
	var parts []string
	parts = append(parts,
		u.styles.timestamp.Render(ts),
		renderLevel(u.styles, level),
		u.styles.brand.Render(Brand),
	)
	if step != "" {
		parts = append(parts, u.styles.step.Render(step))
	}
	parts = append(parts, u.styles.msg.Render(msg))
	if fieldStr != "" {
		parts = append(parts, fieldStr)
	}
	fmt.Fprintln(w, strings.Join(parts, " "))
}

// renderLevel maps a level token to its styled rendering.
func renderLevel(s styles, level string) string {
	switch level {
	case "OK":
		return s.levelOK.Render("OK")
	case "INFO":
		return s.levelInfo.Render("INFO")
	case "WARN":
		return s.levelWarn.Render("WARN")
	case "FAIL":
		return s.levelFail.Render("FAIL")
	default:
		return s.levelInfo.Render(level)
	}
}

// --- Step runner with spinner -------------------------------------------------

// Step announces and runs a single pipeline step. While fn runs an animated
// spinner labelled with step is shown on stderr; when fn returns the spinner
// is replaced by a structured completion record (OK on success, FAIL with the
// error on failure). It returns fn's error so the caller can decide whether
// to abort.
func (u *UI) Step(step, msg string, fn func() error) error {
	if !u.tty {
		u.emit(u.Err, "INFO", step, msg)
		err := fn()
		if err != nil {
			u.emit(u.Err, "FAIL", step, err.Error(), F("err", err.Error()))
			return err
		}
		u.emit(u.Err, "OK", step, "done")
		return nil
	}

	u.startSpinner(step, msg)
	err := fn()
	u.stopSpinner()

	if err != nil {
		u.emit(u.Err, "FAIL", step, err.Error(), F("err", err.Error()))
		return err
	}
	u.emit(u.Err, "OK", step, "done")
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

	ts := u.styles.timestamp.Render(now())
	level := u.styles.levelInfo.Render("··")
	brand := u.styles.brand.Render(Brand)
	stepLbl := u.styles.step.Render(step)
	msgLbl := u.styles.msg.Render(msg)
	fps := u.spin.Spinner.FPS
	if fps <= 0 {
		fps = time.Second / 10 //nolint:mnd
	}

	go func(stop <-chan struct{}, ts, brand, stepLbl, msgLbl string) {
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
				line := fmt.Sprintf("\r%s %s %s %s %s %s", ts, level, brand, frame, stepLbl, msgLbl)
				// pad to clear trailing chars from a previous, longer frame.
				fmt.Fprint(u.Err, line)
				u.mu.Unlock()
			}
		}
	}(u.spinCtx, ts, brand, stepLbl, msgLbl)
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

// --- Progress -----------------------------------------------------------------

// Progress renders a static progress bar at the given ratio (0..1) with an
// optional caption. On a non-TTY it prints a plain structured status line
// instead.
func (u *UI) Progress(caption string, ratio float64) {
	if ratio < 0 {
		ratio = 0
	}
	if ratio > 1 {
		ratio = 1
	}
	if !u.tty {
		u.emit(u.Err, "INFO", "ollama", caption, F("progress", fmt.Sprintf("%3.0f%%", ratio*100)))
		return
	}
	bar := u.progress.ViewAs(ratio)
	cap := u.styles.progress.Render(caption)
	u.println(u.Err, fmt.Sprintf("%s %s %s %s %s  %s",
		u.styles.timestamp.Render(now()),
		u.styles.levelInfo.Render("INFO"),
		u.styles.brand.Render(Brand),
		u.styles.step.Render("ollama"),
		cap,
		bar,
	))
}

// --- Diagnostic / status lines ------------------------------------------------

// Warn prints a structured warning line. The literal token "WARN" is kept so
// downstream consumers (and tests) can grep for it; the plain form also
// preserves the legacy "warning:" substring.
func (u *UI) Warn(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	u.emit(u.Err, "WARN", "", "warning: "+msg)
}

// Info prints a neutral diagnostic line.
func (u *UI) Info(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	u.emit(u.Err, "INFO", "", msg)
}

// Println writes a pre-formatted line to stdout.
func (u *UI) Println(s string) {
	fmt.Fprintln(u.Out, s)
}

// println writes a line terminated string to w.
func (u *UI) println(w io.Writer, s string) {
	fmt.Fprintln(w, s)
}

// --- Final result rendering ---------------------------------------------------

// CommitResult renders the final "committed" structured record to stdout.
// hash is the short commit hash; signed indicates whether the commit object
// carries an SSH signature. The git log is no longer appended — history is
// left to the user's own `git log` invocation.
func (u *UI) CommitResult(hash string, signed bool) {
	u.emit(u.Out, "OK", "commit", "committed",
		F("hash", hash),
		F("signed", fmt.Sprintf("%v", signed)),
	)
}

// TagResult renders the post-commit semver tag notice to stdout. signed
// indicates whether the tag was SSH-signed. The fields are always emitted as
// separate structured key=value pairs (tag, hash, signed) so consumers can
// grep them individually rather than parsing a compressed blob.
func (u *UI) TagResult(name, shortHash string, signed bool) {
	u.emit(u.Out, "OK", "tag", "tagged",
		F("tag", name),
		F("hash", shortHash),
		F("signed", fmt.Sprintf("%v", signed)),
	)
}

// CleanTree renders the "nothing to commit" notice to stderr.
func (u *UI) CleanTree() {
	u.emit(u.Err, "OK", "", "nothing to commit, working tree clean")
}

// Error prints a fatal error to stderr and is used as the final failure
// report before exit.
func (u *UI) Error(err error) {
	u.emit(u.Err, "FAIL", "", err.Error())
}

// FileList renders a compact bullet list of changed file paths under a header.
func (u *UI) FileList(files []string) {
	if len(files) == 0 {
		return
	}
	if !u.tty {
		u.emit(u.Err, "INFO", "diff", "changed files", F("count", fmt.Sprintf("%d", len(files))))
		for _, f := range files {
			fmt.Fprintf(u.Err, "  - %s\n", f)
		}
		return
	}
	header := fmt.Sprintf("%s %s %s %s %s",
		u.styles.timestamp.Render(now()),
		u.styles.levelInfo.Render("INFO"),
		u.styles.brand.Render(Brand),
		u.styles.step.Render("diff"),
		u.styles.muted.Render("changed files:"),
	)
	var lines []string
	lines = append(lines, header)
	for _, f := range files {
		lines = append(lines, fmt.Sprintf("  %s %s", u.styles.bullet.Render("›"), u.styles.fileItem.Render(f)))
	}
	u.println(u.Err, lipgloss.JoinVertical(lipgloss.Left, lines...))
}

// Summary renders the resolved commit subject and (optional) LLM body to
// stdout as a styled preview before the commit is created.
func (u *UI) Summary(subject, body string) {
	if !u.tty {
		u.emit(u.Out, "INFO", "msg", "subject: "+subject)
		if strings.TrimSpace(body) != "" {
			u.emit(u.Out, "INFO", "msg", "body:")
			fmt.Fprintln(u.Out, body)
		}
		return
	}
	head := fmt.Sprintf("%s %s %s %s %s",
		u.styles.timestamp.Render(now()),
		u.styles.levelInfo.Render("INFO"),
		u.styles.brand.Render(Brand),
		u.styles.step.Render("msg"),
		u.styles.muted.Render("commit message:"),
	)
	subj := u.styles.subject.Render(subject)
	block := []string{head, subj}
	if strings.TrimSpace(body) != "" {
		block = append(block, u.styles.body.Render(body))
	}
	u.Println(lipgloss.JoinVertical(lipgloss.Left, block...))
}

// Startup prints the version banner shown at program start. It goes to
// stderr (it is a diagnostic, not program output) as a structured INFO
// record so it remains greppable in plain mode and styled on a TTY. The
// version string is the hardwired/linker-stamped value from
// internal/version (see AGENTS.md "Release / version stamping").
func (u *UI) Startup(version string) {
	u.emit(u.Err, "INFO", "", Brand+" "+version)
}

// SigningNotice renders the signing-on / signing-off notice line.
func (u *UI) SigningNotice(keyPath string, enabled bool) {
	if enabled {
		u.emit(u.Err, "INFO", "sign", "signing commit with ssh key", F("key", keyPath))
		return
	}
	u.emit(u.Err, "OK", "sign", "committing unsigned", F("signed", "false"))
}
