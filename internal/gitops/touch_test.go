package gitops

import (
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	sshpkg "github.com/go-git/go-git/v5/plumbing/transport/ssh"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"

	"paepcke.de/omc/internal/sign"
)

// slowAgent wraps an agent keyring and delays every signature like an
// ssh-agent waiting for a security-key (FIDO2) touch.
type slowAgent struct {
	agent.ExtendedAgent
	delay time.Duration
}

func (a *slowAgent) Sign(key ssh.PublicKey, data []byte) (*ssh.Signature, error) {
	time.Sleep(a.delay)
	return a.ExtendedAgent.Sign(key, data)
}

func (a *slowAgent) SignWithFlags(key ssh.PublicKey, data []byte, flags agent.SignatureFlags) (*ssh.Signature, error) {
	time.Sleep(a.delay)
	return a.ExtendedAgent.SignWithFlags(key, data, flags)
}

// serveSlowAgent stands up a slow agent over a unix socket and points
// SSH_AUTH_SOCK at it, mirroring the cmd/omc agent harness.
func serveSlowAgent(t *testing.T, delay time.Duration) {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	kr := agent.NewKeyring()
	if err := kr.Add(agent.AddedKey{PrivateKey: priv, Comment: "sk stand-in"}); err != nil {
		t.Fatal(err)
	}
	ea, ok := kr.(agent.ExtendedAgent)
	if !ok {
		t.Fatal("agent keyring does not implement ExtendedAgent")
	}
	l, err := net.Listen("unix", filepath.Join(t.TempDir(), "agent.sock"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				_ = agent.ServeAgent(&slowAgent{ExtendedAgent: ea, delay: delay}, c)
			}(c)
		}
	}()
	t.Setenv("SSH_AUTH_SOCK", l.Addr().String())
}

// skHandle writes a security-key handle (public half in the .pub file, like
// a real id_ed25519_sk pair) matching the agent identity created by
// serveSlowAgent, and returns its path.
func skHandle(t *testing.T) string {
	t.Helper()
	// Re-derive the agent's view: list its identities.
	// (The test creates the key inside serveSlowAgent; to keep the two in
	// sync, skHandle re-reads it from the agent.)
	conn, err := net.Dial("unix", os.Getenv("SSH_AUTH_SOCK"))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	ag := agent.NewClient(conn)
	signers, err := ag.Signers()
	if err != nil || len(signers) == 0 {
		t.Fatalf("agent has no identities: %v", err)
	}
	handle := filepath.Join(t.TempDir(), "id_ed25519_sk")
	if err := os.WriteFile(handle+".pub", ssh.MarshalAuthorizedKey(signers[0].PublicKey()), 0o600); err != nil {
		t.Fatal(err)
	}
	return handle
}

// TestSecurityKeyAuthTouchTimeout verifies that SSH transport signers built
// for a FIDO2 push key are wrapped by the touch policy: a signature that
// blocks longer than the configured timeout fails with sign.ErrTouchTimeout
// instead of hanging the fetch/push forever — the exact class of hang that
// hit the pre-tag upstream pull.
func TestSecurityKeyAuthTouchTimeout(t *testing.T) {
	serveSlowAgent(t, 500*time.Millisecond)
	handle := skHandle(t)

	auth, err := securityKeyAuth("git", handle, sign.TouchConfig{
		Timeout: 50 * time.Millisecond,
		Action:  "the push",
	})
	if err != nil {
		t.Fatal(err)
	}
	pk, ok := auth.(*sshpkg.PublicKeysCallback)
	if !ok {
		t.Fatalf("securityKeyAuth returned %T, want *sshpkg.PublicKeysCallback", auth)
	}
	signers, err := pk.Callback()
	if err != nil {
		t.Fatal(err)
	}
	if len(signers) == 0 {
		t.Fatal("no signers offered for the sk handle")
	}

	start := time.Now()
	_, err = signers[0].Sign(rand.Reader, []byte("session payload"))
	if !errors.Is(err, sign.ErrTouchTimeout) {
		t.Fatalf("expected ErrTouchTimeout, got %v", err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("timeout did not bound the SSH auth wait, took %s", elapsed)
	}
}

// TestSecurityKeyAuthInactiveTouchConfigIsTransparent verifies that an empty
// TouchConfig leaves the offered signers untouched: the signature still
// succeeds (after the agent's delay), preserving the old behavior for
// callers that pass a zero config (e.g. the tests of plain push paths).
func TestSecurityKeyAuthInactiveTouchConfigIsTransparent(t *testing.T) {
	serveSlowAgent(t, 50*time.Millisecond)
	handle := skHandle(t)

	auth, err := securityKeyAuth("git", handle, sign.TouchConfig{})
	if err != nil {
		t.Fatal(err)
	}
	pk := auth.(*sshpkg.PublicKeysCallback)
	signers, err := pk.Callback()
	if err != nil {
		t.Fatal(err)
	}
	sig, err := signers[0].Sign(rand.Reader, []byte("session payload"))
	if err != nil {
		t.Fatalf("inactive touch config must not interfere with signing: %v", err)
	}
	if sig == nil {
		t.Fatal("expected a signature")
	}
}
