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
// long-running steps such as the two-stage LLM generation. Related pipeline
// steps are gathered into logical log groups (BeginGroup / EndGroup): a
// colored group header is printed and every record inside the group is
// prefixed with a tree connector (┌─ first, ├─ middle, └─ last) tying the
// steps together, and groups may nest (see BeginGroup) so the whole run
// forms one readable tree — e.g. "preparing repository" → "committing &
// publishing" → "signing with security key". The resolved commit message is
// shown inside a rounded-border frame with a light background (renderFrame)
// so it stands out from the surrounding log lines. Diagnostics and the live
// spinner go to stderr; the final commit and tag results go to stdout,
// preserving the Unix convention that stdout carries program output.
//
// When stderr is not a terminal (piped output, captured tests, CI logs) the
// colored rendering, tree grouping, and frame are all suppressed and omc
// falls back to the plain structured line format so the output stays
// greppable and deterministic.
package output

import (
	"fmt"
	"io"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/term"

	"paepcke.de/omc/internal/notify"
)

// Brand is the application label shown in every structured log line.
const Brand = "omc"

// palette holds the lipgloss colors used across the UI.
var palette = struct {
	brand       lipgloss.Color
	subject     lipgloss.Color
	success     lipgloss.Color
	warn        lipgloss.Color
	err         lipgloss.Color
	muted       lipgloss.Color
	accent      lipgloss.Color
	detail      lipgloss.Color
	timestamp   lipgloss.Color
	info        lipgloss.Color
	step        lipgloss.Color
	key         lipgloss.Color
	val         lipgloss.Color
	spinner     lipgloss.Color
	frameBg     lipgloss.Color
	frameBorder lipgloss.Color
	frameTitle  lipgloss.Color
	tree        lipgloss.Color
}{
	brand:       lipgloss.Color("#7D56F4"),
	subject:     lipgloss.Color("#EE6FF8"),
	success:     lipgloss.Color("#3FB950"),
	warn:        lipgloss.Color("#D29922"),
	err:         lipgloss.Color("#F85149"),
	muted:       lipgloss.Color("#6E7681"),
	accent:      lipgloss.Color("#58A6FF"),
	detail:      lipgloss.Color("#A371F7"),
	timestamp:   lipgloss.Color("#6E7681"),
	info:        lipgloss.Color("#58A6FF"),
	step:        lipgloss.Color("#A371F7"),
	key:         lipgloss.Color("#6E7681"),
	val:         lipgloss.Color("#C9D1D9"),
	spinner:     lipgloss.Color("#EE6FF8"),
	frameBg:     lipgloss.Color("#1C2128"),
	frameBorder: lipgloss.Color("#7D56F4"),
	frameTitle:  lipgloss.Color("#7D56F4"),
	tree:        lipgloss.Color("#A371F7"),
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
	// width is the detected terminal width (in cells) at construction time,
	// or 0 when it could not be determined. Used to size the commit-message
	// frame so it follows the real terminal width instead of the content's
	// natural width.
	width int

	mu       sync.Mutex
	anim     *anim
	animOn   bool
	animCtx  chan struct{}
	animStep string
	// touch is the security-key touch wait currently being prompted (see
	// TouchBegin in touch.go). The signers trigger it from wherever the
	// smartcard blocks — commit signing, tag signing, the upstream tag
	// pull, the push — so the prompt is always tied to the actual wait.
	touch *touchPrompt
	// notifier is the runtime-detected desktop notification backend used
	// by touch prompts (see SetNotifier); nil disables them.
	notifier notify.Notifier

	// groups holds the stack of active logical log groups (the colored
	// trees that tie related pipeline steps together; see BeginGroup).
	// When the stack is non-empty each structured line emitted on a TTY
	// is prefixed with a tree connector (├─ or └─) that reflects every
	// open group, and each group's header has already been rendered.
	// BeginGroup pushes; EndGroup pops. Groups are a no-op outside a TTY
	// so captured logs keep the flat, greppable form.
	groups []group
}

// group is a single level of the nested log-group tree (see UI.groups).
// title is the colored header text rendered when the group opened;
// remaining counts the structured lines still expected inside the group
// (the connector for the last line is └─, the others ├─); when remaining
// reaches zero the group closes itself.
type group struct {
	title     string
	remaining int
	total     int
}

type styles struct {
	brand        lipgloss.Style
	timestamp    lipgloss.Style
	levelOK      lipgloss.Style
	levelInfo    lipgloss.Style
	levelWarn    lipgloss.Style
	levelFail    lipgloss.Style
	step         lipgloss.Style
	msg          lipgloss.Style
	key          lipgloss.Style
	val          lipgloss.Style
	muted        lipgloss.Style
	bullet       lipgloss.Style
	fileItem     lipgloss.Style
	frameBox     lipgloss.Style
	frameSubject lipgloss.Style
	frameBody    lipgloss.Style
	frameTitle   lipgloss.Style
	tree         lipgloss.Style
}

// New returns a UI writing to the passed streams. TTY detection is performed
// on stderr: when stderr is a terminal the styled, animated UI is used;
// otherwise the plain text fallback is used. The terminal width is probed at
// the same time so the commit-message frame can be sized to the real
// terminal width (capped at 80) instead of the content's natural width.
func New(out, err io.Writer) *UI {
	tty := false
	width := 0
	if f, ok := err.(*os.File); ok {
		fd := uintptr(f.Fd())
		if term.IsTerminal(fd) {
			tty = true
			if w, _, err := term.GetSize(fd); err == nil && w > 0 {
				width = w
			}
		}
	}
	ui := &UI{
		Out:   out,
		Err:   err,
		tty:   tty,
		width: width,
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
		muted:     base.Foreground(palette.muted),
		bullet:    base.Foreground(palette.accent),
		fileItem:  base.Foreground(palette.accent),
		frameBox: base.
			Border(lipgloss.RoundedBorder()).
			BorderForeground(palette.frameBorder).
			Background(palette.frameBg).
			Padding(0, 1),
		frameSubject: base.Bold(true).Foreground(palette.subject).Background(palette.frameBg),
		frameBody:    base.Foreground(palette.detail).Background(palette.frameBg),
		frameTitle:   base.Bold(true).Foreground(palette.frameTitle).Background(palette.frameBg),
		tree:         base.Foreground(palette.tree),
	}
}

// BeginGroup opens a logical log group. On a TTY a colored group header is
// printed and subsequent structured records are prefixed with a tree
// connector (├─ / └─) tying them under that header. lineCount is the number
// of structured lines that will be emitted inside the group; the connector
// for the last line is └─, the others ├─. Groups may nest: opening a group
// while another is active renders the new header indented under the parent's
// tree, and every line is prefixed with one connector per open depth (so a
// record emitted inside a nested group carries a "├─ ├─ ..." prefix that
// mirrors its position in the overall tree). Off a TTY BeginGroup is a no-op
// so captured logs keep their flat, greppable form.
func (u *UI) BeginGroup(title string, lineCount int) {
	if !u.tty || lineCount <= 0 {
		return
	}
	g := group{
		title:     title,
		remaining: lineCount,
		total:     lineCount,
	}
	u.groups = append(u.groups, g)
	u.renderGroupHeader()
}

// renderGroupHeader prints the colored header of the group just pushed,
// prefixed with the parent tree connectors so nested group headers line up
// like the records that follow them. The header is a structured, timestamped
// line: it carries the same leading timestamp as every structured record so
// the whole tree reads as one consistent log. The parent connectors (when
// nested) mirror the position the child records will occupy, and the title
// replaces the level/brand/step tokens a record would carry at that depth.
func (u *UI) renderGroupHeader() {
	ts := u.styles.timestamp.Render(now())
	top := u.groups[len(u.groups)-1]
	title := u.styles.step.Render(top.title)
	if n := len(u.groups); n > 1 {
		var b strings.Builder
		for i := range n - 1 {
			b.WriteString(u.groupTree(u.groups[i]))
			b.WriteByte(' ')
		}
		line := fmt.Sprintf("%s %s  %s", ts, b.String(), title)
		fmt.Fprintln(u.Err, line)
		return
	}
	line := fmt.Sprintf("%s   %s", ts, title)
	fmt.Fprintln(u.Err, line)
}

// EndGroup closes the innermost active log group. On a TTY it prints a blank
// line as a visual separator when the whole tree closes (so nested groups
// read as contiguous subtrees); off a TTY it is a no-op. A group whose line
// budget is exhausted is auto-closed by emit (see emit); EndGroup is for
// groups closed early or for the outer group when you want the separator.
func (u *UI) EndGroup() {
	if !u.tty {
		return
	}
	popped := false
	if n := len(u.groups); n > 0 {
		u.groups = u.groups[:n-1]
		popped = true
	}
	if popped && len(u.groups) == 0 {
		fmt.Fprintln(u.Err)
	}
}

// frameMaxWidth caps the commit-message frame width. Even on very wide
// terminals a commit message line longer than 80 cells is hard to read, so
// the frame never grows beyond this.
const frameMaxWidth = 80

// frameFallbackWidth is the assumed terminal width when the real width
// could not be detected (e.g. a pseudo-terminal without a size ioctl). It
// matches frameMaxWidth so the frame is readable out of the box.
const frameFallbackWidth = 80

// frameContentBudget returns the inner content width (the number of cells
// available for text inside the frame) given an outer frame width. The
// rounded border occupies 2 cells (one each side) and the 0,1 padding
// adds another 2 cells, so the budget is outer - 4.
func frameContentBudget(outerWidth int) int {
	budget := outerWidth - 4
	if budget < 1 {
		return 1
	}
	return budget
}

// frameWidth returns the outer width the commit-message frame should target:
// the detected terminal width capped at frameMaxWidth, or frameFallbackWidth
// when the terminal width is unknown. The result is always >= 8 so the
// border + padding always leave room for at least a few characters of text.
func (u *UI) frameWidth() int {
	w := u.width
	if w <= 0 {
		w = frameFallbackWidth
	}
	if w > frameMaxWidth {
		w = frameMaxWidth
	}
	if w < 8 {
		w = 8
	}
	return w
}

// renderFrame returns the commit message rendered inside a rounded-border
// box on a light background, with a small "commit message" title sitting on
// top of the box. Used by Summary on a TTY to make the generated/override
// commit message stand out from the surrounding structured log lines.
//
// The box is sized to the real terminal width (probed at construction time
// and capped at 80 cells) so it does not collapse to the content's natural
// width on short messages; when the terminal width is unknown an 80-cell
// fallback is used. The subject and body are wrapped to the box's inner
// content width so long lines break instead of stretching the frame.
func (u *UI) renderFrame(subject, body string) string {
	outer := u.frameWidth()
	budget := frameContentBudget(outer)

	subj := u.styles.frameSubject.
		Width(budget).
		Render("💬 " + subject)

	content := subj
	if strings.TrimSpace(body) != "" {
		bodyBlock := u.styles.frameBody.
			Width(budget).
			Render(body)
		content = lipgloss.JoinVertical(lipgloss.Left, content, "", bodyBlock)
	}
	box := u.styles.frameBox.
		Width(outer - 2). // border consumes 2 cells; this sets the inner width
		Render(content)
	header := "  └ " + u.styles.frameTitle.Render("commit message")
	return lipgloss.JoinVertical(lipgloss.Left, header, box)
}

// --- structured log line rendering -------------------------------------------

// now returns the current time formatted as HH:MM:SS.
func now() string { return time.Now().Format("15:04:05") }

// renderFields renders a slice of key=value fields as "key=val key=val".
// Values are quoted with strconv.Quote when they contain whitespace, `"`,
// or a non-printable rune, so a multi-word error or action label cannot
// break the space-separated record into ambiguous tokens.
func (u *UI) renderFields(fields []Field) string {
	if len(fields) == 0 {
		return ""
	}
	var parts []string
	for _, f := range fields {
		val := fieldValue(f.Val)
		if u.tty {
			parts = append(parts, fmt.Sprintf("%s=%s",
				u.styles.key.Render(f.Key),
				u.styles.val.Render(val)))
		} else {
			parts = append(parts, fmt.Sprintf("%s=%s", f.Key, val))
		}
	}
	return strings.Join(parts, " ")
}

// fieldValue renders a field value so it stays a single token: values that
// contain whitespace, non-printable runes, or `"` are quoted.
func fieldValue(v string) string {
	if v == "" {
		return v
	}
	if strings.ContainsRune(v, '"') || strings.ContainsFunc(v, func(r rune) bool {
		return unicode.IsSpace(r) || !unicode.IsPrint(r)
	}) {
		return strconv.Quote(v)
	}
	return v
}

// emit writes a structured log record to w. level is one of OK/INFO/WARN/FAIL.
// step is the pipeline step name (may be empty); msg is the human message;
// fields are optional key=value pairs. The record always carries a leading
// timestamp and the brand name so consumers can grep on "omc" regardless
// of TTY mode.
//
// On a TTY, when a logical log group is active (see BeginGroup), the record
// is prefixed with a colored tree connector (├─ for in-group lines, └─ for
// the last line of the group) tying related steps together under the
// group's header. Outside a group, or off a TTY, the flat form is emitted.
func (u *UI) emit(w io.Writer, level, step, msg string, fields ...Field) {
	// The animated countdown / spinner goroutines rewrite the current
	// stderr line under u.mu; every structured write (here and theirs) goes
	// through the same mutex so a record printed while a countdown is live
	// cannot interleave with the rewrites and get garbled or erased.
	u.mu.Lock()
	defer u.mu.Unlock()
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
	if u.inGroup() {
		// Render with the pre-consumption counters (remaining includes the
		// current line, so a group's first line renders ┌─ and its last
		// └─), then consume one line from every open group and auto-close
		// groups whose line budget is exhausted. Popping from the top
		// keeps the remaining stack consistent with the nesting; when the
		// pops empty the whole tree a separator blank is printed so
		// groups read as contiguous subtrees.
		fmt.Fprintln(w, u.renderGroupedLine(ts, level, step, msg, fieldStr))
		for i := range u.groups {
			if u.groups[i].remaining > 0 {
				u.groups[i].remaining--
			}
		}
		closed := false
		for len(u.groups) > 0 && u.groups[len(u.groups)-1].remaining <= 0 {
			u.groups = u.groups[:len(u.groups)-1]
			closed = true
		}
		if closed && len(u.groups) == 0 {
			fmt.Fprintln(u.Err)
		}
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

// inGroup reports whether at least one log group is currently open.
func (u *UI) inGroup() bool { return len(u.groups) > 0 }

// groupTree returns the colored tree connector for a single open group:
// ┌─ for the first line emitted inside the group, ├─ for middle lines, └─
// for the last line. A single-line group renders └─ (the line is both
// first and last). The connector is derived from the group's remaining
// counter, so it reflects how many lines are still expected inside the
// group.
func (u *UI) groupTree(g group) string {
	first := g.remaining == g.total
	last := g.remaining == 1
	var s string
	switch {
	case g.total == 1:
		s = "└─"
	case first:
		s = "┌─"
	case last:
		s = "└─"
	default:
		s = "├─"
	}
	return u.styles.tree.Render(s)
}

// renderGroupedTree returns the full colored "connector prefix" for the
// current line: one connector per open group, joined by a space, reflecting
// the line's position in every open tree level. A record emitted inside a
// nested group therefore carries a "├─ ├─" style prefix that mirrors its
// depth, like git log --graph.
func (u *UI) renderGroupedTree() string {
	var b strings.Builder
	for i := range u.groups {
		if i > 0 {
			b.WriteByte(' ')
		}
		b.WriteString(u.groupTree(u.groups[i]))
	}
	return b.String()
}

// renderGroupedLine renders a single structured log record prefixed with the
// colored group tree connector. The line keeps the same components as the
// flat TTY form (timestamp · level · brand · step · message · fields) so the
// text tokens (OK / INFO / step name / etc.) stay greppable.
func (u *UI) renderGroupedLine(ts, level, step, msg, fieldStr string) string {
	var parts []string
	parts = append(parts,
		u.styles.timestamp.Render(ts),
		u.renderGroupedTree(),
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
	return strings.Join(parts, " ")
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
	if u.tty {
		u.startSpinner(step, msg)
	} else {
		u.emit(u.Err, "INFO", step, msg)
	}
	err := fn()
	if u.tty {
		u.stopSpinner()
	}

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
	// When the spinner is animating inside an active log group, render the
	// same connector prefix as the structured line that will replace it so
	// the spinner line stays visually aligned with its grouped completion
	// record.
	tree := ""
	if u.inGroup() {
		tree = " " + u.renderGroupedTree()
	}
	fps := time.Second / animFPS

	go func(stop <-chan struct{}, ts, tree, level, brand, stepLbl string) {
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
				line := fmt.Sprintf("\r%s%s %s %s %s %s", ts, tree, level, brand, stepLbl, frame)
				fmt.Fprint(u.Err, line)
				u.mu.Unlock()
			}
		}
	}(u.animCtx, ts, tree, level, brand, stepLbl)
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
		u.animStep = step
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

// --- Final result rendering ---------------------------------------------------

// CommitResult renders the final "committed" structured record to stdout.
// hash is the short commit hash; signed indicates whether the commit object
// carries an SSH signature. The git log is no longer appended — history is
// left to the user's own `git log` invocation.
func (u *UI) CommitResult(hash string, signed bool) {
	u.emit(u.Out, "OK", "commit", "committed",
		F("hash", hash),
		F("signed", strconv.FormatBool(signed)),
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
		F("signed", strconv.FormatBool(signed)),
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
	fmt.Fprintln(u.Err, lipgloss.JoinVertical(lipgloss.Left, lines...))
}

// Summary renders the resolved commit subject and (optional) LLM body to
// stdout as a styled preview before the commit is created. On a TTY the
// message is placed inside a rounded-border frame with a light background
// so the message the user is about to commit is unmissable; off a TTY the
// flat "subject:" / "body:" structured form is kept for greppability.
func (u *UI) Summary(subject, body string) {
	if !u.tty {
		u.emit(u.Out, "INFO", "msg", "subject: "+subject)
		if strings.TrimSpace(body) != "" {
			u.emit(u.Out, "INFO", "msg", "body:")
			fmt.Fprintln(u.Out, body)
		}
		return
	}
	fmt.Fprintln(u.Out, u.renderFrame(subject, body))
}

// Startup prints the version banner shown at program start. It goes to
// stderr (it is a diagnostic, not program output) as a structured INFO
// record so it remains greppable in plain mode and styled on a TTY. The
// version string is the hardwired/linker-stamped value from
// internal/version (see AGENTS.md "Release / version stamping").
func (u *UI) Startup(version string) {
	u.emit(u.Err, "INFO", "", version)
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

// RepoContextNotice emits the repository context diagnostic shown right
// after the open step succeeds: the containing directory (worktree root,
// where .git lives), the latest semver tag detected, and the configured
// remotes (name → URLs). The remotes are emitted as one field per URL with a
// stable, sorted key (<remote>_url, or <remote>_url_<i> when a remote has
// multiple URLs) so consumers can grep push/pull targets individually.
// Emitted as a structured INFO record with the "repo" step so it lines up
// in the "preparing repository" group tree.
func (u *UI) RepoContextNotice(dir, latestTag string, remotes map[string][]string) {
	fields := []Field{F("dir", dir)}
	if latestTag == "" {
		fields = append(fields, F("latest_tag", "(none)"))
	} else {
		fields = append(fields, F("latest_tag", latestTag))
	}
	names := make([]string, 0, len(remotes))
	for n := range remotes {
		names = append(names, n)
	}
	slices.Sort(names)
	if len(names) == 0 {
		fields = append(fields, F("remotes", "(none)"))
	}
	for _, n := range names {
		urls := remotes[n]
		for i, url := range urls {
			key := n + "_url"
			if len(urls) > 1 {
				key = fmt.Sprintf("%s_url_%d", n, i)
			}
			fields = append(fields, F(key, url))
		}
	}
	u.emit(u.Err, "INFO", "repo", "detected repository context", fields...)
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
