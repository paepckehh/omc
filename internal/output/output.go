// Package output centralizes all user-facing formatting for omc.
//
// It renders a structured, timestamped terminal UI built on lipgloss. Every
// line emitted — diagnostics, the live scramble spinner, and final results —
// is a structured log record of the form:
//
//	<HH:MM:SS> <LEVEL> omc [<step>] <message> [key=value ...]
//
// Examples (plain, non-TTY form):
//
//	12:04:07 INFO  omc open   detecting repository
//	12:04:07 OK    omc stage  done
//	12:04:08 WARN  omc        ssh key unusable  key=/x err="nope"
//	12:04:09 OK    omc commit committed     hash=9d3f2ab signed=true
//	12:04:09 OK    omc tag    tagged         tag=v0.0.3 hash=9d3f2ab signed=true
//
// When stderr is a terminal the same records are rendered with color, icons,
// aligned columns, and an animated scramble spinner (see anim.go) for
// long-running steps such as the two-stage LLM generation. Diagnostics and
// the live spinner go to stderr; the final commit and tag results go to
// stdout, preserving the Unix convention that stdout carries program output.
//
// When stderr is not a terminal (piped output, captured tests, CI logs) the
// colored rendering is suppressed and omc falls back to the plain
// structured line format so the output stays greppable and deterministic.
package output

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/term"
)

// Brand is the application label shown in every structured log line.
const Brand = "omc"

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

	tty    bool
	styles styles

	mu       sync.Mutex
	anim     *anim
	animOn   bool
	animCtx  chan struct{}
	animStep string
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
// timestamp and the brand name so consumers can grep on "omc" regardless
// of TTY mode.
func (u *UI) emit(w io.Writer, level, step, msg string, fields ...Field) {
	ts := now()
	fieldStr := u.renderFields(fields)
	if !u.tty {
		var b strings.Builder
		b.WriteString(ts)
		b.WriteByte(' ')
		if e := StateEmoji(level); e != "" {
			b.WriteString(e)
			b.WriteByte(' ')
		}
		b.WriteString(level)
		b.WriteString(" ")
		b.WriteString(Brand)
		if step != "" {
			b.WriteByte(' ')
			if e := ActionEmoji(step); e != "" {
				b.WriteString(e)
				b.WriteByte(' ')
			}
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
		parts = append(parts, u.styles.step.Render(stepIcon(step)))
	}
	parts = append(parts, u.styles.msg.Render(msg))
	if fieldStr != "" {
		parts = append(parts, fieldStr)
	}
	fmt.Fprintln(w, strings.Join(parts, " "))
}

// renderLevel maps a level token to its styled rendering, decorated with
// the matching state emoji.
func renderLevel(s styles, level string) string {
	if e := StateEmoji(level); e != "" {
		return s.levelInfo.Render(e) + " " + levelToken(s, level)
	}
	return levelToken(s, level)
}

// levelToken renders the bare level token in its colored style.
func levelToken(s styles, level string) string {
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

// stepIcon returns the decorated step name "icon step" used by the styled
// log line, falling back to the bare step name when no icon exists.
func stepIcon(step string) string {
	if e := ActionEmoji(step); e != "" {
		return e + " " + step
	}
	return step
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

// startSpinner launches a goroutine that animates the scramble spinner on
// stderr. Only one spinner may be active at a time. The label can be changed
// live via updateSpinner.
func (u *UI) startSpinner(step, msg string) {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.animOn {
		return
	}
	u.animOn = true
	u.animStep = step
	u.anim = newAnim(animSettings{
		label:      msg,
		labelColor: palette.spinner,
		gradA:      palette.brand,
		gradB:      palette.subject,
		size:       animDefaultSize,
		seed:       uint64(time.Now().UnixNano()),
	})
	u.animCtx = make(chan struct{})

	ts := u.styles.timestamp.Render(now())
	level := u.styles.levelInfo.Render(StatePending + " ··")
	brand := u.styles.brand.Render(Brand)
	stepLbl := u.styles.step.Render(stepIcon(step))
	fps := time.Second / animFPS

	go func(stop <-chan struct{}, ts, level, brand, stepLbl string) {
		tk := time.NewTicker(fps)
		defer tk.Stop()
		for {
			select {
			case <-stop:
				return
			case <-tk.C:
				u.mu.Lock()
				if !u.animOn || u.anim == nil {
					u.mu.Unlock()
					return
				}
				u.anim.tick()
				frame := u.anim.render()
				line := fmt.Sprintf("\r%s %s %s %s %s", ts, level, brand, stepLbl, frame)
				fmt.Fprint(u.Err, line)
				u.mu.Unlock()
			}
		}
	}(u.animCtx, ts, level, brand, stepLbl)
}

// updateSpinner changes the live label of the running spinner. Safe to call
// while the spinner goroutine is animating.
func (u *UI) updateSpinner(msg string) {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.anim != nil {
		u.anim.setLabel(msg)
	}
}

// stopSpinner stops the active spinner goroutine and clears its line.
func (u *UI) stopSpinner() {
	u.mu.Lock()
	if !u.animOn {
		u.mu.Unlock()
		return
	}
	u.animOn = false
	close(u.animCtx)
	u.animCtx = nil
	u.anim = nil
	// Clear the spinner line.
	fmt.Fprint(u.Err, "\r\033[K")
	u.mu.Unlock()
}

// --- Live spinner (LLM generation) --------------------------------------------

// SpinnerStart begins an animated scramble spinner for a long-running
// asynchronous step such as the two-stage Ollama generation. Pair it with
// SpinnerUpdate and SpinnerStop. On a non-TTY it emits a structured INFO
// line instead of animating, keeping captured logs greppable.
func (u *UI) SpinnerStart(step, msg string) {
	if !u.tty {
		u.emit(u.Err, "INFO", step, msg)
		return
	}
	u.startSpinner(step, msg)
}

// SpinnerUpdate changes the live label of the running spinner (e.g. moving
// from "generating commit message" to "condensing to TL;DR"). On a non-TTY
// it emits a fresh structured INFO line for the new phase.
func (u *UI) SpinnerUpdate(msg string) {
	if !u.tty {
		u.emit(u.Err, "INFO", u.animStep, msg)
		return
	}
	u.updateSpinner(msg)
}

// SpinnerStop stops the running spinner and clears its line. On a non-TTY it
// is a no-op; the structured announce/complete lines carry the state.
func (u *UI) SpinnerStop() {
	if !u.tty {
		return
	}
	u.stopSpinner()
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
		u.styles.levelInfo.Render("ℹ️ INFO"),
		u.styles.brand.Render(Brand),
		u.styles.step.Render(stepIcon("diff")),
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
		u.styles.levelInfo.Render("ℹ️ INFO"),
		u.styles.brand.Render(Brand),
		u.styles.step.Render(stepIcon("msg")),
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

// ConfigNotice prints the startup environment/configuration summary to
// stderr, immediately following the version banner. detected lists the
// environment variables that were set (non-empty); verified lists the
// config options that passed a startup verification. Both are emitted as
// structured INFO records with key=value fields so consumers can grep them.
func (u *UI) ConfigNotice(detected, verified []Field) {
	if len(detected) > 0 {
		fields := append([]Field{F("count", fmt.Sprintf("%d", len(detected)))}, detected...)
		u.emit(u.Err, "INFO", "config", "detected environment", fields...)
	}
	if len(verified) > 0 {
		fields := append([]Field{F("count", fmt.Sprintf("%d", len(verified)))}, verified...)
		u.emit(u.Err, "INFO", "config", "verified config", fields...)
	}
}

// SigningNotice renders the signing-on / signing-off notice line.
func (u *UI) SigningNotice(keyPath string, enabled bool) {
	if enabled {
		u.emit(u.Err, "INFO", "sign", "signing commit with ssh key", F("key", keyPath))
		return
	}
	u.emit(u.Err, "OK", "sign", "committing unsigned", F("signed", "false"))
}

// SecurityKeyModeNotice renders the notice line for security-key (FIDO2)
// signing through the ssh-agent: signing is enabled, but delegated to the
// agent and the smartcard (the touch check happens on the device). The
// signing algorithm, when non-empty, is emitted as a field so users can see
// the sk-* algorithm in effect.
func (u *UI) SecurityKeyModeNotice(keyPath, algorithm string) {
	if algorithm == "" {
		u.emit(u.Err, "INFO", "sign", "signing commit with ssh-agent security key", F("key", keyPath), F("mode", "smartcard"))
		return
	}
	u.emit(u.Err, "INFO", "sign", "signing commit with ssh-agent security key", F("key", keyPath), F("mode", "smartcard"), F("algo", algorithm))
}

// PushResult renders the post-push notice to stdout: the remote that was
// pushed to, the pushed branch (empty on detached HEAD), and whether the
// tag pass ran. The fields are emitted as separate key=value tokens so
// consumers can grep them.
func (u *UI) PushResult(remote, branch string, tags bool) {
	u.emit(u.Out, "OK", "push", "pushed",
		F("remote", remote),
		F("branch", branch),
		F("tags", fmt.Sprintf("%v", tags)),
	)
}
