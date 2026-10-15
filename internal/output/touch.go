package output

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/lipgloss"

	"paepcke.de/omc/internal/notify"
	"paepcke.de/omc/internal/touch"
)

// The touch wait prompt. When a signature goes to the smartcard (FIDO2
// security key), the signer wrapper calls TouchBegin before the blocking
// agent request and TouchEnd when it returns. The prompt layer mirrors the
// proven patterns of OpenSSH (its "Confirm user presence for key ..." notice
// plus askpass when there is no TTY) and yubikey-agent (a deferred desktop
// notification, so a user who is already touching never sees one):
//
//   - a structured INFO record announces the wait (both TTY and non-TTY, so
//     captured logs stay greppable);
//   - on a TTY an animated countdown replaces any running spinner and a
//     terminal bell rings, so the wait is unmistakable even when the commit
//     tool runs in a background window;
//   - after a few seconds without a touch, a desktop notification is posted
//     through the runtime-detected org.freedesktop.Notifications backend
//     (GNOME Shell, KDE Plasma, XFCE, ...) — the same channel notify-send
//     uses, implemented in pure Go over D-Bus;
//   - off a TTY periodic progress records keep long waits visible in logs;
//   - TouchEnd stops everything, closes the notification, and reports the
//     outcome: touch confirmed, timed out, or failed.

const (
	// touchCountdownFPS is the animation frame rate for the countdown.
	touchCountdownFPS = 10
	// countdownBarWidth is the number of half-block cells used for the
	// progress bar shown alongside the countdown.
	countdownBarWidth = 16
	// touchCountdownDefault bounds the animated countdown when the wait
	// itself is unbounded (OMC_TOUCH_TIMEOUT=0): the display then counts
	// down to zero and holds, while the wait continues indefinitely.
	touchCountdownDefault = 30 * time.Second
	// touchProgressInterval is how often the non-TTY path emits a progress
	// record while waiting for a touch.
	touchProgressInterval = 5 * time.Second
	// touchBell rings the terminal attention character on TTYs when the
	// wait starts.
	touchBell = "\a"
)

// touchNotifyDelay defers the desktop notification: a user who is already
// touching the key (the common case) never sees one. A var (not a const)
// so tests can shorten it.
var touchNotifyDelay = 5 * time.Second

// touchPrompt is the state of the single active touch wait. omc signs
// sequentially, so at most one prompt is live at a time; a Begin while
// another wait is still open closes the old one silently first.
type touchPrompt struct {
	ev      touch.Event
	started time.Time
	// done is closed when the wait ends; every animation goroutine
	// selects on it.
	done chan struct{}
	// closeOnce guards the close of done.
	closeOnce sync.Once
	// notifyID is the desktop notification id once posted (0 before),
	// guarded by the UI mutex.
	notifyID uint32
	// notifyPosted marks that the deferred notification goroutine got
	// past its delay; guarded by the UI mutex.
	notifyPosted bool
}

// SetNotifier installs the desktop notification backend used by touch
// prompts. Pass the result of notify.Detect (nil when no desktop
// notification daemon is reachable, which is the default) before the first
// prompt appears. After a prompt has started, the change applies to the
// next one.
func (u *UI) SetNotifier(n notify.Notifier) {
	u.mu.Lock()
	u.notifier = n
	u.mu.Unlock()
}

// Notifier returns the currently installed desktop notification backend
// (nil when none).
func (u *UI) Notifier() notify.Notifier {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.notifier
}

// TouchBegin implements touch.Hook: it starts the prompt for a smartcard
// touch wait. See the package comment for the full behavior.
func (u *UI) TouchBegin(ev touch.Event) {
	u.mu.Lock()
	if u.touch != nil {
		// Defensive: the previous wait never ended (cannot happen with
		// omc's sequential signing); close it silently and move on.
		old := u.touch
		u.touch = nil
		old.closeOnce.Do(func() { close(old.done) })
	}
	p := &touchPrompt{ev: ev, started: time.Now(), done: make(chan struct{})}
	u.touch = p
	u.mu.Unlock()

	// The structured record always fires, TTY or not, and always before
	// the blocking wait: it is what makes a hung touch greppable in
	// captured logs.
	fields := []Field{F("key", ev.KeyPath)}
	if ev.Algorithm != "" {
		fields = append(fields, F("algo", ev.Algorithm))
	}
	if ev.Action != "" {
		fields = append(fields, F("action", ev.Action))
	}
	if ev.Timeout > 0 {
		fields = append(fields, F("timeout", ev.Timeout.String()))
	}
	u.emit(u.Err, "INFO", "touch",
		"waiting for security key touch, touch your smartcard/yubikey when it blinks",
		fields...)

	if u.tty {
		// The countdown owns the stderr line for the duration of the
		// wait: any spinner running for the enclosing step is stopped
		// (its completion record follows on a fresh line later).
		u.stopSpinner()
		u.bell()
		u.startCountdown(p)
	} else {
		u.startTouchProgress(p)
	}
	// The notification delay is captured now, on the caller's goroutine,
	// so the goroutine below never races a test shortening it.
	u.startTouchNotification(p, touchNotifyDelay)
}

// TouchEnd implements touch.Hook: it resolves the prompt started by
// TouchBegin, reporting the outcome of the wait. A nil Err means the
// smartcard answered (touch observed); a timeout or failure is reported as
// a WARN record so the degrade path is visible in the logs.
func (u *UI) TouchEnd(ev touch.Event) {
	u.mu.Lock()
	p := u.touch
	u.touch = nil
	var (
		notifyID    uint32
		notifyShown bool
	)
	if p != nil {
		p.closeOnce.Do(func() { close(p.done) })
		notifyID, notifyShown = p.notifyID, p.notifyPosted
	}
	notifier := u.notifier
	if u.tty {
		// Clear the countdown line before the records below render.
		fmt.Fprint(u.Err, "\r\033[K")
	}
	u.mu.Unlock()
	if p == nil {
		return
	}
	if notifier != nil && notifyShown && notifyID != 0 {
		go notifier.Close(notifyID)
	}

	elapsed := time.Since(p.started).Round(time.Second)
	if u.tty {
		// A direct, tree-aligned line matching the structured record
		// emitted just below, without consuming a line from any open
		// group's budget — the structured emit owns that.
		ts := u.styles.timestamp.Render(now())
		tree := ""
		if u.inGroup() {
			tree = " " + u.renderGroupedTree()
		}
		var msg string
		switch {
		case ev.TimedOut:
			msg = lipgloss.NewStyle().
				Bold(true).
				Foreground(palette.err).
				Render("⏰ touch timed out, waiting for security key")
		case ev.Err != nil:
			msg = lipgloss.NewStyle().
				Bold(true).
				Foreground(palette.warn).
				Render("⚠️  security key wait ended with an error")
		default:
			msg = lipgloss.NewStyle().
				Bold(true).
				Foreground(palette.success).
				Render("💛 touch confirmed, thank you!")
		}
		fmt.Fprintf(u.Err, "\r\033[K%s%s %s\n", ts, tree, msg)
	}

	fields := []Field{
		F("elapsed", elapsed.String()),
		F("key", p.ev.KeyPath),
	}
	if p.ev.Algorithm != "" {
		fields = append(fields, F("algo", p.ev.Algorithm))
	}
	if p.ev.Action != "" {
		fields = append(fields, F("action", p.ev.Action))
	}
	switch {
	case ev.TimedOut:
		fields = append(fields,
			F("outcome", "timeout"),
			F("timeout", p.ev.Timeout.String()),
			F("err", ev.Err.Error()),
		)
		u.emit(u.Err, "WARN", "touch", "touch timed out waiting for security key", fields...)
	case ev.Err != nil:
		fields = append(fields, F("outcome", "error"), F("err", ev.Err.Error()))
		u.emit(u.Err, "WARN", "touch", "security key wait ended with an error", fields...)
	default:
		fields = append(fields, F("outcome", "confirmed"))
		u.emit(u.Err, "OK", "touch", "touch confirmed, thank you", fields...)
	}
}

// bell rings the terminal attention character on TTYs.
func (u *UI) bell() {
	if u.tty {
		fmt.Fprint(u.Err, touchBell)
	}
}

// startCountdown launches the animated countdown goroutine for p. The
// countdown runs until the wait ends; when the wait is unbounded it counts
// down to the default display budget and holds at zero.
func (u *UI) startCountdown(p *touchPrompt) {
	total := p.ev.Timeout
	if total <= 0 {
		total = touchCountdownDefault
	}
	cd := &touchCountdown{total: total.Seconds(), remaining: total.Seconds()}
	fps := time.Second / touchCountdownFPS
	go func() {
		tk := time.NewTicker(fps)
		defer tk.Stop()
		for {
			select {
			case <-p.done:
				return
			case <-tk.C:
				u.mu.Lock()
				if u.touch != p {
					u.mu.Unlock()
					return
				}
				cd.remaining -= fps.Seconds()
				if cd.remaining < 0 {
					cd.remaining = 0
				}
				fmt.Fprintf(u.Err, "\r\033[K%s", cd.render())
				u.mu.Unlock()
			}
		}
	}()
}

// startTouchProgress launches the non-TTY progress goroutine: a structured
// record every touchProgressInterval keeps long waits visible in captured
// logs.
func (u *UI) startTouchProgress(p *touchPrompt) {
	go func() {
		tk := time.NewTicker(touchProgressInterval)
		defer tk.Stop()
		for {
			select {
			case <-p.done:
				return
			case <-tk.C:
				u.mu.Lock()
				stale := u.touch != p
				u.mu.Unlock()
				if stale {
					return
				}
				elapsed := time.Since(p.started)
				fields := []Field{
					F("key", p.ev.KeyPath),
					F("elapsed", elapsed.Round(time.Second).String()),
				}
				if p.ev.Timeout > 0 {
					remaining := p.ev.Timeout - elapsed
					if remaining < 0 {
						remaining = 0
					}
					fields = append(fields, F("remaining", remaining.Round(time.Second).String()))
				}
				if p.ev.Action != "" {
					fields = append(fields, F("action", p.ev.Action))
				}
				u.emit(u.Err, "INFO", "touch", "still waiting for security key touch", fields...)
			}
		}
	}()
}

// startTouchNotification launches the deferred desktop notification for p:
// if the wait is still pending after delay, a notification is posted
// through the installed backend; it is closed again when the wait ends (see
// TouchEnd). A missing backend is a no-op.
func (u *UI) startTouchNotification(p *touchPrompt, delay time.Duration) {
	go func() {
		select {
		case <-p.done:
			return
		case <-time.After(delay):
		}
		u.mu.Lock()
		stale := u.touch != p
		notifier := u.notifier
		if !stale && notifier != nil {
			p.notifyPosted = true
		}
		u.mu.Unlock()
		if stale || notifier == nil {
			return
		}
		summary := "omc: security key touch required"
		body := "Touch your smartcard/YubiKey to authorise " + p.ev.Action
		if p.ev.Action == "" {
			body = "Touch your smartcard/YubiKey to authorise the signature"
		}
		if p.ev.Timeout > 0 {
			body += fmt.Sprintf(" (waiting up to %s)", p.ev.Timeout)
		}
		expire := p.ev.Timeout
		if expire <= 0 {
			expire = 30 * time.Second
		}
		id := notifier.Notify(summary, body, "dialog-password", expire)
		if id == 0 {
			return
		}
		u.mu.Lock()
		if u.touch == p {
			p.notifyID = id
		} else {
			// The wait ended while the notification was being posted:
			// close it right away.
			go notifier.Close(id)
		}
		u.mu.Unlock()
	}()
}

// touchCountdown renders the animated countdown line: a key glyph, a
// shrinking progress bar, and the time remaining, all on one line.
type touchCountdown struct {
	total     float64
	remaining float64
}

// render returns the styled countdown line.
func (c *touchCountdown) render() string {
	elapsed := c.total - c.remaining
	frac := 0.0
	if c.total > 0 {
		frac = elapsed / c.total
	}
	frac = max(0, min(1, frac))
	full := min(int(frac*float64(countdownBarWidth)), countdownBarWidth)
	empty := countdownBarWidth - full

	var bar strings.Builder
	bar.Grow(countdownBarWidth)
	bar.WriteString(strings.Repeat("▰", full))
	bar.WriteString(strings.Repeat("▱", empty))
	barStyled := lipgloss.NewStyle().Foreground(palette.warn).Render(bar.String())
	key := lipgloss.NewStyle().Bold(true).Foreground(palette.warn).Render("🔑 TOUCH YOUR SECURITY KEY")
	timer := lipgloss.NewStyle().Bold(true).Foreground(palette.err).Render("⏱ " + c.fmtSeconds())
	return fmt.Sprintf("%s  %s  %s", key, barStyled, timer)
}

// fmtSeconds renders the remaining time as an M:SS string.
func (c *touchCountdown) fmtSeconds() string {
	s := max(0, int(c.remaining))
	return fmt.Sprintf("%d:%02d", s/60, s%60)
}

var _ touch.Hook = (*UI)(nil)
