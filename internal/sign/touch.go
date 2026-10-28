package sign

import (
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"

	"paepcke.de/omc/internal/touch"
)

// ErrTouchTimeout is returned by a touch-wrapped signer when the configured
// touch wait budget elapsed before the smartcard produced a signature.
// Callers degrade exactly like for any other unusable signing key: warn and
// proceed unsigned, never block.
var ErrTouchTimeout = errors.New("security key touch wait timed out")

// TouchState latches the first failed security-key touch wait of a run.
// omc never loops touch prompts: once a wait has ended in a timeout or an
// error, every later signature through a TouchConfig sharing this state
// fails immediately with ErrTouchTimeout — no prompt, no hook events, no
// wait — so the run degrades the remaining smartcard operations in one go
// and can exit non-zero. A confirmed touch does not latch.
type TouchState struct {
	mu     sync.Mutex
	failed bool
}

// Failed reports whether a touch wait has already failed in this run.
func (s *TouchState) Failed() bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.failed
}

// latch records the failed wait. Idempotent; only the first failure matters.
// A nil state (no gate configured) is a no-op.
func (s *TouchState) latch() {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.failed = true
	s.mu.Unlock()
}

// TouchConfig configures the touch wait wrapper that sign.WrapTouch (and the
// agent-backed signers built on it) install around every smartcard-bound
// signature. Zero fields disable the wrapper's extras: no hook, no timeout
// (the signature then blocks until the agent returns, which for a security
// key means until the user touches, possibly forever, matching OpenSSH's
// own unbounded behavior).
type TouchConfig struct {
	// KeyPath names the on-disk key handle for prompts and records. Filled
	// in automatically by the constructors that know it.
	KeyPath string
	// Action names the operation the signature authorises (e.g. "the
	// commit signing"). Callers may override it per signature via
	// Signer.SignAs.
	Action string
	// Algorithm is the sk-* signature algorithm reported to the hook; it
	// defaults to the algorithm the wrapped signer actually produces.
	Algorithm string
	// Timeout is the maximum wait for the touch. Zero means unbounded.
	Timeout time.Duration
	// Hook receives the begin/end of every touch wait. May be nil.
	Hook touch.Hook
	// State, when set, is shared by every TouchConfig of the run: the
	// first failed touch wait latches it and all later waits fail fast
	// without prompting (see TouchState). May be nil.
	State *TouchState
}

// Active reports whether the wrapper has anything to do: a hook to notify
// or a timeout to enforce. Inactive configs wrap to a plain pass-through.
func (c TouchConfig) Active() bool {
	return c.Hook != nil || c.Timeout > 0
}

// For returns a copy of the config labelled for a specific operation (e.g.
// "the commit signing"), keeping the timeout and hook. It is the one-liner
// callers use to thread the shared policy into each signing site.
func (c TouchConfig) For(action string) TouchConfig {
	c.Action = action
	return c
}

// WrapTouch wraps an ssh.Signer so that every signature request it handles
// is treated as a security-key touch wait: the configured touch.Hook is
// notified before the (blocking) inner Sign starts and again when it
// returns, and a positive Timeout bounds the wait by failing the signature
// with ErrTouchTimeout instead of blocking forever. The wrapped signer
// forwards PublicKey and, when the inner signer supports it, algorithm
// selection, so it can replace the inner signer anywhere an ssh.Signer is
// expected (sshsig signing, SSH transport authentication).
//
// On timeout the underlying agent request is abandoned (the ssh-agent
// protocol has no cancellation): the goroutine stays blocked until the
// agent answers, but its result is discarded and the caller degrades. The
// agent connection is not reused afterwards in that case.
func WrapTouch(inner ssh.Signer, cfg TouchConfig) ssh.Signer {
	if inner == nil {
		return nil
	}
	if cfg.Algorithm == "" {
		cfg.Algorithm = skTypeOf(inner)
	}
	return &touchSigner{inner: inner, cfg: cfg}
}

// touchSigner is an ssh.Signer that adds touch-wait prompting and an
// enforced timeout around every signature of an agent-backed security-key
// identity. The agent connection stays alive through the underlying
// signer; it is deliberately never closed here, the short-lived CLI exits
// right after signing and the OS cleans up.
type touchSigner struct {
	inner ssh.Signer
	cfg   TouchConfig

	mu     sync.Mutex
	action string
}

var (
	_ ssh.Signer          = (*touchSigner)(nil)
	_ ssh.AlgorithmSigner = (*touchSigner)(nil)
)

// setAction overrides the action label reported to the touch hook for the
// next signature (see Signer.SignAs).
func (s *touchSigner) setAction(action string) {
	s.mu.Lock()
	s.action = action
	s.mu.Unlock()
}

func (s *touchSigner) PublicKey() ssh.PublicKey { return s.inner.PublicKey() }

func (s *touchSigner) Sign(rand io.Reader, data []byte) (*ssh.Signature, error) {
	return s.sign(rand, data, "")
}

// SignWithAlgorithm forwards algorithm selection to the inner signer when
// it supports it; a plain inner signer only accepts an empty algorithm (or
// its own key type).
func (s *touchSigner) SignWithAlgorithm(rand io.Reader, data []byte, algorithm string) (*ssh.Signature, error) {
	if algorithm == "" || algorithm == s.inner.PublicKey().Type() {
		return s.sign(rand, data, "")
	}
	if _, ok := s.inner.(ssh.AlgorithmSigner); !ok {
		return nil, fmt.Errorf("signer %q does not support algorithm %q", s.inner.PublicKey().Type(), algorithm)
	}
	return s.sign(rand, data, algorithm)
}

// sign performs one touch-wrapped signature. The inner Sign runs on its own
// goroutine (the ssh-agent protocol blocks until the smartcard answers and
// offers no cancellation); the enforced deadline races the result and, on
// timeout, fails the signature with ErrTouchTimeout.
//
// When the shared TouchState is already latched (an earlier wait in this
// run failed), the signature fails immediately with ErrTouchTimeout: the
// user already declined to touch, so omc never loops the prompt — no hook
// events fire, the caller degrades like for any unusable key, and the run
// exits non-zero.
func (s *touchSigner) sign(rand io.Reader, data []byte, algorithm string) (*ssh.Signature, error) {
	if st := s.cfg.State; st.Failed() {
		return nil, fmt.Errorf("%w: an earlier security key wait already failed; not prompting again", ErrTouchTimeout)
	}
	s.mu.Lock()
	action := s.action
	s.mu.Unlock()
	if action == "" {
		action = s.cfg.Action
	}

	type result struct {
		sig *ssh.Signature
		err error
	}
	resCh := make(chan result, 1)
	go func() {
		var r result
		if algorithm == "" {
			r.sig, r.err = s.inner.Sign(rand, data)
		} else {
			r.sig, r.err = s.inner.(ssh.AlgorithmSigner).SignWithAlgorithm(rand, data, algorithm)
		}
		resCh <- r
	}()

	if s.cfg.Hook != nil {
		s.cfg.Hook.TouchBegin(touch.Event{
			KeyPath:   s.cfg.KeyPath,
			Action:    action,
			Algorithm: s.cfg.Algorithm,
			Timeout:   s.cfg.Timeout,
		})
	}

	var (
		r        result
		timedOut bool
	)
	if s.cfg.Timeout > 0 {
		timer := time.NewTimer(s.cfg.Timeout)
		select {
		case r = <-resCh:
			timer.Stop()
		case <-timer.C:
			timedOut = true
			r = result{err: fmt.Errorf("%w after %s", ErrTouchTimeout, s.cfg.Timeout)}
		}
	} else {
		r = <-resCh
	}

	if r.err != nil {
		// The wait failed (timeout or error): latch the shared state so
		// no later smartcard operation in this run prompts again.
		s.cfg.State.latch()
	}
	if s.cfg.Hook != nil {
		s.cfg.Hook.TouchEnd(touch.Event{
			KeyPath:   s.cfg.KeyPath,
			Action:    action,
			Algorithm: s.cfg.Algorithm,
			Timeout:   s.cfg.Timeout,
			Err:       r.err,
			TimedOut:  timedOut,
		})
	}
	return r.sig, r.err
}
