package output

import (
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"paepcke.de/omc/internal/touch"
)

// fakeNotifier records the desktop notifications posted through it, for
// asserting the deferred-notification behavior of touch prompts.
type fakeNotifier struct {
	mu       sync.Mutex
	shown    []string
	closedID []uint32
}

func (f *fakeNotifier) Notify(summary, body, icon string, expire time.Duration) uint32 {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.shown = append(f.shown, summary+"|"+body+"|"+icon)
	return uint32(len(f.shown))
}

func (f *fakeNotifier) Close(id uint32) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closedID = append(f.closedID, id)
}

func (f *fakeNotifier) Describe() string { return "fake" }

func TestTouchBeginEndNonTTY(t *testing.T) {
	ui, _, errBuf := newTestUI()
	ui.TouchBegin(touch.Event{
		KeyPath:   "/k/id_ed25519_sk",
		Action:    "the commit signing",
		Algorithm: "sk-ssh-ed25519@openssh.com",
		Timeout:   10 * time.Second,
	})
	ui.TouchEnd(touch.Event{Err: nil})

	got := errBuf.String()
	for _, want := range []string{
		"INFO", "touch",
		"waiting for security key touch",
		"smartcard/yubikey",
		"key=/k/id_ed25519_sk",
		"algo=sk-ssh-ed25519@openssh.com",
		"action=\"the commit signing\"",
		"timeout=10s",
		"OK", "touch confirmed, thank you",
		"outcome=confirmed",
		"elapsed=",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in touch records, got: %q", want, got)
		}
	}
	// The begin record must precede the end record in the captured log.
	if strings.Index(got, "waiting for security key touch") > strings.Index(got, "touch confirmed, thank you") {
		t.Errorf("begin record must precede end record, got: %q", got)
	}
}

func TestTouchTimeoutNonTTY(t *testing.T) {
	ui, _, errBuf := newTestUI()
	ui.TouchBegin(touch.Event{KeyPath: "/k/id_ed25519_sk", Action: "the push", Timeout: 5 * time.Second})
	ui.TouchEnd(touch.Event{Err: errors.New("security key touch wait timed out after 5s"), TimedOut: true, Timeout: 5 * time.Second})

	got := errBuf.String()
	for _, want := range []string{
		"WARN",
		"touch timed out waiting for security key",
		"outcome=timeout",
		"timeout=5s",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in timeout records, got: %q", want, got)
		}
	}
	if strings.Contains(got, "touch confirmed") {
		t.Errorf("timeout must not report a confirmed touch, got: %q", got)
	}
}

func TestTouchErrorNonTTY(t *testing.T) {
	ui, _, errBuf := newTestUI()
	ui.TouchBegin(touch.Event{KeyPath: "/k/id_ed25519_sk"})
	ui.TouchEnd(touch.Event{Err: errors.New("agent refused")})

	got := errBuf.String()
	if !strings.Contains(got, "security key wait ended with an error") {
		t.Errorf("missing error outcome record, got: %q", got)
	}
	if !strings.Contains(got, "outcome=error") || !strings.Contains(got, `err="agent refused"`) {
		t.Errorf("missing structured outcome/err fields, got: %q", got)
	}
}

func TestTouchPromptOnTTY(t *testing.T) {
	ui, _, errBuf := newTestTTYUI()
	ui.TouchBegin(touch.Event{KeyPath: "/k/id_ed25519_sk", Action: "the tag signing"})
	ui.TouchEnd(touch.Event{})

	got := stripANSI(errBuf.String())
	if !strings.Contains(got, "waiting for security key touch") {
		t.Errorf("missing begin record on TTY, got: %q", got)
	}
	if !strings.Contains(got, "💛 touch confirmed, thank you!") {
		t.Errorf("missing TTY confirmation line, got: %q", got)
	}
	if !strings.Contains(got, "outcome=confirmed") {
		t.Errorf("missing structured confirmed record, got: %q", got)
	}
	// The bell must ring exactly once per wait.
	if bells := strings.Count(errBuf.String(), "\a"); bells != 1 {
		t.Errorf("expected exactly one bell, got %d", bells)
	}
}

func TestTouchTimeoutOnTTY(t *testing.T) {
	ui, _, errBuf := newTestTTYUI()
	ui.TouchBegin(touch.Event{KeyPath: "/k/id_ed25519_sk"})
	ui.TouchEnd(touch.Event{Err: errors.New("timed out"), TimedOut: true})

	got := stripANSI(errBuf.String())
	if !strings.Contains(got, "⏰ touch timed out") {
		t.Errorf("missing TTY timeout line, got: %q", got)
	}
	if !strings.Contains(got, "outcome=timeout") {
		t.Errorf("missing structured timeout record, got: %q", got)
	}
}

func TestTouchNotificationDeferredAndClosed(t *testing.T) {
	orig := touchNotifyDelay
	touchNotifyDelay = 50 * time.Millisecond
	defer func() { touchNotifyDelay = orig }()

	ui, _, _ := newTestUI()
	fn := &fakeNotifier{}
	ui.SetNotifier(fn)

	// Fast touch (ends before the delay): the notification never fires.
	ui.TouchBegin(touch.Event{KeyPath: "/k/id_ed25519_sk", Action: "the push"})
	ui.TouchEnd(touch.Event{})
	time.Sleep(150 * time.Millisecond)
	fn.mu.Lock()
	shown := len(fn.shown)
	fn.mu.Unlock()
	if shown != 0 {
		t.Fatalf("fast touch must not post a desktop notification, got %d", shown)
	}

	// Slow touch: the notification fires after the delay and is closed on
	// end.
	ui.TouchBegin(touch.Event{KeyPath: "/k/id_ed25519_sk", Action: "the push", Timeout: 60 * time.Second})
	time.Sleep(2 * touchNotifyDelay)
	ui.TouchEnd(touch.Event{})
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		fn.mu.Lock()
		closed := len(fn.closedID)
		fn.mu.Unlock()
		if closed > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	fn.mu.Lock()
	defer fn.mu.Unlock()
	if len(fn.shown) != 1 {
		t.Fatalf("expected one desktop notification, got: %v", fn.shown)
	}
	if !strings.Contains(fn.shown[0], "the push") {
		t.Errorf("notification body must name the action, got: %q", fn.shown[0])
	}
	if !strings.Contains(fn.shown[0], "dialog-password") {
		t.Errorf("notification must carry a password-dialog icon, got: %q", fn.shown[0])
	}
	if len(fn.closedID) != 1 {
		t.Errorf("notification must be closed on end, got: %v", fn.closedID)
	}
}

func TestTouchNotificationDisabledWithoutNotifier(t *testing.T) {
	// No notifier installed: nothing must panic or block, on TTY and off.
	ui, _, errBuf := newTestUI()
	ui.TouchBegin(touch.Event{KeyPath: "/k/id_ed25519_sk"})
	ui.TouchEnd(touch.Event{})
	if !strings.Contains(errBuf.String(), "waiting for security key touch") {
		t.Errorf("records must still be emitted without a notifier, got: %q", errBuf.String())
	}
}

func TestTouchCountdownRender(t *testing.T) {
	cd := &touchCountdown{total: 30, remaining: 30}
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

	cd.remaining = 15
	got = cd.render()
	if !strings.Contains(got, "0:15") {
		t.Errorf("missing 0:15 after advance, got: %q", got)
	}
	if !strings.Contains(got, "▰") || !strings.Contains(got, "▱") {
		t.Errorf("missing mixed progress bar cells, got: %q", got)
	}

	cd.remaining = 0
	got = cd.render()
	if !strings.Contains(got, "0:00") {
		t.Errorf("missing 0:00 at zero, got: %q", got)
	}
	if !strings.Contains(got, "▰") {
		t.Errorf("missing filled progress bar cells at zero, got: %q", got)
	}
}

// Compile-time proof that the UI satisfies the hook the signers drive.
var _ touch.Hook = (*UI)(nil)
