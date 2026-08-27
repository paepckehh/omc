// Package output: animated scramble spinner.
//
// anim is a self-contained, ticker-driven spinner inspired by
// charmbracelet/crush's internal/ui/anim. It renders a row of
// gradient-colored, scrambling runes that flow into a text label with an
// animated trailing ellipsis (., .., ..., ""). A short staggered "birth"
// fade-in plays on start: each column shows a placeholder glyph until its
// randomly assigned birth step is reached, after which the scramble takes
// over and the ellipsis begins.
//
// Unlike crush's version this implementation has no bubbletea dependency:
// the owner drives it by calling Tick() then Render() on each timer tick,
// which fits omc's goroutine + carriage-return line-rewrite UI model.

package output

import (
	"math/rand/v2"
	"strings"

	"github.com/charmbracelet/lipgloss"
	colorful "github.com/lucasb-eyer/go-colorful"
)

const (
	// animFPS is the animation frame rate. At 20 FPS a frame advances every
	// 50ms.
	animFPS = 20
	// animEllipsisSpeed is the number of frames each ellipsis frame is held
	// for. At 20 FPS, 8 frames = 400ms per dot state.
	animEllipsisSpeed = 8
	// animMaxBirth is the maximum number of frames before a column's birth
	// step. After this many frames every column has activated and the
	// ellipsis animation begins. At 20 FPS this is ~1s of staggered
	// entrance.
	animMaxBirth = 20
	// animNumFrames is the number of prerendered scramble frames; the
	// animation loops after this many steps.
	animNumFrames = 10
	// animDefaultSize is the default number of cycling scramble columns.
	animDefaultSize = 10
	// animGap is the separator between the scramble and the label.
	animGap = " "
)

var (
	// animRunes is the alphabet picked from when scrambling each column.
	animRunes = []rune("0123456789abcdefABCDEF~!@#$%^&*()+=_")
	// animEllipsis is the four-frame trailing-dot animation.
	animEllipsis = []string{".", "..", "...", ""}
)

// animSettings configures an anim spinner.
type animSettings struct {
	label      string
	labelColor lipgloss.Color
	gradA      lipgloss.Color
	gradB      lipgloss.Color
	size       int
	seed       uint64
}

// anim is an animated scramble spinner. All frames are prerendered at
// construction time so Tick()/Render() do no lipgloss work on the hot path.
type anim struct {
	settings animSettings
	size     int

	ramp        []lipgloss.Color // per-column gradient color
	scramble    [][]string       // [frame][col] prerendered colored rune
	initialChar []string         // [col] prerendered birth glyph
	labelRunes  []string         // prerendered label runes
	ellipsis    []string         // prerendered ellipsis frames
	birthSteps  []int            // [col] frame at which the column activates

	labelWidth int

	step        int  // current scramble frame (wraps)
	frames      int  // total ticks since start (does not wrap)
	ellipsisSt  int  // current ellipsis frame counter (wraps)
	initialized bool // true once the birth animation has completed
}

// newAnim builds a prerendered anim spinner from opts.
func newAnim(opts animSettings) *anim {
	if opts.size < 1 {
		opts.size = animDefaultSize
	}
	a := &anim{
		settings: opts,
		size:     opts.size,
	}
	a.labelWidth = lipgloss.Width(opts.label)

	// Gradient ramp across the scramble columns, blended in Hcl space to
	// stay in gamut (matching crush's makeGradientRamp).
	a.ramp = blendColors(opts.gradA, opts.gradB, a.size)

	// Prerender the scramble frames. The rune picker is seeded off opts.seed
	// so two spinners built from the same settings scramble identically.
	rng := rand.New(rand.NewPCG(opts.seed, ^opts.seed))
	a.scramble = make([][]string, animNumFrames)
	for f := range a.scramble {
		a.scramble[f] = make([]string, a.size)
		for c := range a.scramble[f] {
			r := animRunes[rng.IntN(len(animRunes))]
			a.scramble[f][c] = lipgloss.NewStyle().Foreground(a.ramp[c]).Render(string(r))
		}
	}

	// Prerender the birth glyph (a single dot) for each column.
	a.initialChar = make([]string, a.size)
	for c := range a.initialChar {
		a.initialChar[c] = lipgloss.NewStyle().Foreground(a.ramp[c]).Render(".")
	}

	a.renderLabel(opts.label)

	// Prerender the ellipsis frames in the label color.
	a.ellipsis = make([]string, len(animEllipsis))
	for i, e := range animEllipsis {
		a.ellipsis[i] = lipgloss.NewStyle().Foreground(opts.labelColor).Render(e)
	}

	// Assign each column a deterministic birth step for the staggered
	// entrance, seeded independently from the scramble rng.
	birthRng := rand.New(rand.NewPCG(opts.seed^0x9E3779B97F4A7C15, ^opts.seed))
	a.birthSteps = make([]int, a.size)
	for i := range a.birthSteps {
		a.birthSteps[i] = birthRng.IntN(animMaxBirth)
	}

	return a
}

// renderLabel prerenders the label runes in the label color.
func (a *anim) renderLabel(label string) {
	a.settings.label = label
	a.labelWidth = lipgloss.Width(label)
	a.labelRunes = a.labelRunes[:0]
	for _, r := range label {
		a.labelRunes = append(a.labelRunes,
			lipgloss.NewStyle().Foreground(a.settings.labelColor).Render(string(r)))
	}
}

// setLabel updates the label text live and re-renders its runes.
func (a *anim) setLabel(label string) {
	a.renderLabel(label)
}

// tick advances the animation by one frame.
func (a *anim) tick() {
	a.step = (a.step + 1) % animNumFrames
	a.frames++
	if a.initialized {
		a.ellipsisSt = (a.ellipsisSt + 1) % (animEllipsisSpeed * len(animEllipsis))
	} else if a.frames >= animMaxBirth {
		a.initialized = true
	}
}

// render returns the current frame as a styled string: scramble columns,
// a gap, the label, and (once initialized) the animated ellipsis.
func (a *anim) render() string {
	var b strings.Builder
	for i := 0; i < a.size; i++ {
		if !a.initialized && a.frames < a.birthSteps[i] {
			b.WriteString(a.initialChar[i])
		} else {
			b.WriteString(a.scramble[a.step][i])
		}
	}
	if a.labelWidth > 0 {
		b.WriteString(animGap)
		for _, r := range a.labelRunes {
			b.WriteString(r)
		}
		if a.initialized {
			b.WriteString(a.ellipsis[a.ellipsisSt/animEllipsisSpeed])
		}
	}
	return b.String()
}

// blendColors returns a slice of lipgloss colors blended from a to b in
// steps, using Hcl interpolation so the gradient stays perceptually smooth
// and in gamut. Returns nil if either color cannot be parsed or steps <= 0.
func blendColors(a, b lipgloss.Color, steps int) []lipgloss.Color {
	ca, errA := colorful.Hex(string(a))
	cb, errB := colorful.Hex(string(b))
	if errA != nil || errB != nil || steps <= 0 {
		return nil
	}
	out := make([]lipgloss.Color, steps)
	for i := range steps {
		t := 0.0
		if steps > 1 {
			t = float64(i) / float64(steps-1)
		}
		c := ca.BlendHcl(cb, t).Clamped()
		out[i] = lipgloss.Color(c.Hex())
	}
	return out
}
