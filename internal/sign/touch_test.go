package sign

import (
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"io"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

	"paepcke.de/omc/internal/touch"
)

// recordingHook captures the begin/end events of a touch wait, mirroring what
// the UI will receive.
type recordingHook struct {
	mu     sync.Mutex
	events []touch.Event
}

func (h *recordingHook) TouchBegin(e touch.Event) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.events = append(h.events, e)
}

func (h *recordingHook) TouchEnd(e touch.Event) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.events = append(h.events, e)
}

func (h *recordingHook) snapshot() []touch.Event {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]touch.Event(nil), h.events...)
}

func softwareSigner(t *testing.T) ssh.Signer {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	s, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// blockingSigner emulates a smartcard-bound agent identity that waits for a
// user touch: Sign blocks until release is closed.
type blockingSigner struct {
	inner   ssh.Signer
	release chan struct{}
}

func (s *blockingSigner) PublicKey() ssh.PublicKey { return s.inner.PublicKey() }

func (s *blockingSigner) Sign(rand io.Reader, data []byte) (*ssh.Signature, error) {
	<-s.release
	return s.inner.Sign(rand, data)
}

func TestWrapTouchPassesSignatureThrough(t *testing.T) {
	inner := softwareSigner(t)
	hook := &recordingHook{}
	wrapped := WrapTouch(inner, TouchConfig{
		KeyPath: "/k/id_ed25519_sk",
		Action:  "the commit signing",
		Hook:    hook,
	})

	data := []byte("payload")
	sig, err := wrapped.Sign(rand.Reader, data)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	want, err := inner.Sign(rand.Reader, data)
	if err != nil {
		t.Fatal(err)
	}
	if sig.Format != want.Format {
		t.Errorf("signature format altered by wrapper: %q vs %q", sig.Format, want.Format)
	}

	events := hook.snapshot()
	if len(events) != 2 {
		t.Fatalf("expected begin+end events, got %d: %+v", len(events), events)
	}
	if events[0].KeyPath != "/k/id_ed25519_sk" || events[0].Action != "the commit signing" {
		t.Errorf("begin event lost key/action: %+v", events[0])
	}
	if events[1].Err != nil || events[1].TimedOut {
		t.Errorf("end event must be a confirmed touch: %+v", events[1])
	}
}

func TestWrapTouchTimeout(t *testing.T) {
	release := make(chan struct{})
	defer close(release)
	inner := &blockingSigner{inner: softwareSigner(t), release: release}
	hook := &recordingHook{}
	wrapped := WrapTouch(inner, TouchConfig{
		KeyPath: "/k/id_ed25519_sk",
		Action:  "the push",
		Timeout: 50 * time.Millisecond,
		Hook:    hook,
	})

	start := time.Now()
	_, err := wrapped.Sign(rand.Reader, []byte("payload"))
	if !errors.Is(err, ErrTouchTimeout) {
		t.Fatalf("expected ErrTouchTimeout, got %v", err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("timeout did not bound the wait, took %s", elapsed)
	}

	events := hook.snapshot()
	if len(events) != 2 {
		t.Fatalf("expected begin+end events, got %d: %+v", len(events), events)
	}
	end := events[1]
	if !end.TimedOut || end.Err == nil {
		t.Errorf("end event must carry the timeout: %+v", end)
	}
	if end.Timeout != 50*time.Millisecond {
		t.Errorf("end event lost the enforced timeout: %+v", end)
	}
}

func TestWrapTouchUnboundedWaitsForever(t *testing.T) {
	// Timeout 0 keeps OpenSSH's behavior: no timeout is enforced, the
	// signature completes whenever the touch arrives.
	release := make(chan struct{})
	inner := &blockingSigner{inner: softwareSigner(t), release: release}
	wrapped := WrapTouch(inner, TouchConfig{Hook: &recordingHook{}})

	go func() {
		time.Sleep(80 * time.Millisecond)
		close(release)
	}()
	done := make(chan error, 1)
	go func() {
		_, err := wrapped.Sign(rand.Reader, []byte("payload"))
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("unbounded sign failed: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("unbounded sign never completed")
	}
}

func TestTouchConfigForAndActive(t *testing.T) {
	if (TouchConfig{}).Active() {
		t.Error("zero config (no hook, no timeout) should be inactive")
	}
	base := TouchConfig{Timeout: time.Second, Hook: &recordingHook{}}
	labeled := base.For("the tag signing")
	if labeled.Action != "the tag signing" {
		t.Errorf("For must set the action, got %q", labeled.Action)
	}
	if labeled.Timeout != time.Second {
		t.Errorf("For must keep the timeout, got %v", labeled.Timeout)
	}
	if (TouchConfig{Timeout: time.Second}).Active() != true {
		t.Error("timeout alone must make the config active")
	}
}

func TestSignerSignAsLabelsTouchWait(t *testing.T) {
	inner := softwareSigner(t)
	hook := &recordingHook{}
	s := &Signer{signer: WrapTouch(inner, TouchConfig{Hook: hook})}

	if _, err := s.SignAs("the tag signing", []byte("payload")); err != nil {
		t.Fatalf("SignAs: %v", err)
	}
	events := hook.snapshot()
	if len(events) == 0 {
		t.Fatal("expected touch events from SignAs")
	}
	if got := events[0].Action; got != "the tag signing" {
		t.Errorf("SignAs label not applied, got %q", got)
	}
}

// TestTouchStateFailFastAfterFailure pins the no-loop contract: once a
// touch wait has failed (here: timed out), the shared TouchState latches
// and every later signature through the same policy fails immediately —
// no second prompt, no touch events, no wait — so a run never loops the
// smartcard prompt after the user already failed to touch.
func TestTouchStateFailFastAfterFailure(t *testing.T) {
	release := make(chan struct{})
	defer close(release)
	inner := &blockingSigner{inner: softwareSigner(t), release: release}
	hook := &recordingHook{}
	state := &TouchState{}
	wrapped := WrapTouch(inner, TouchConfig{
		KeyPath: "/k/id_ed25519_sk",
		Action:  "the push",
		Timeout: 40 * time.Millisecond,
		Hook:    hook,
		State:   state,
	})

	if _, err := wrapped.Sign(rand.Reader, []byte("payload")); !errors.Is(err, ErrTouchTimeout) {
		t.Fatalf("first sign must fail with ErrTouchTimeout, got %v", err)
	}
	if !state.Failed() {
		t.Fatal("TouchState must latch after a failed touch wait")
	}

	start := time.Now()
	_, err := wrapped.Sign(rand.Reader, []byte("payload"))
	if !errors.Is(err, ErrTouchTimeout) {
		t.Fatalf("second sign must fail fast with ErrTouchTimeout, got %v", err)
	}
	if elapsed := time.Since(start); elapsed > 20*time.Millisecond {
		t.Fatalf("gated sign must not wait for the touch, took %s", elapsed)
	}
	if events := hook.snapshot(); len(events) != 2 {
		t.Fatalf("gated sign must not fire touch events (no re-prompt), got %d: %+v", len(events), events)
	}
}

// TestTouchStateSuccessDoesNotLatch pins the opposite side: a confirmed
// touch must not latch the gate — the next smartcard operation still gets
// its own prompt and its own bounded wait.
func TestTouchStateSuccessDoesNotLatch(t *testing.T) {
	hook := &recordingHook{}
	state := &TouchState{}
	wrapped := WrapTouch(softwareSigner(t), TouchConfig{
		KeyPath: "/k/id_ed25519_sk",
		Hook:    hook,
		State:   state,
	})

	if _, err := wrapped.Sign(rand.Reader, []byte("payload")); err != nil {
		t.Fatalf("sign: %v", err)
	}
	if state.Failed() {
		t.Fatal("a confirmed touch must not latch the gate")
	}
	if events := hook.snapshot(); len(events) != 2 {
		t.Fatalf("expected begin+end events, got %d", len(events))
	}
}
