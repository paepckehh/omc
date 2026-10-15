package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-git/go-git/v5"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
)

// neverSignAgent wraps an agent keyring and blocks every signature for far
// longer than any test, emulating a security key nobody touches.
type neverSignAgent struct {
	agent.ExtendedAgent
}

func (a neverSignAgent) Sign(key ssh.PublicKey, data []byte) (*ssh.Signature, error) {
	time.Sleep(10 * time.Minute)
	return a.ExtendedAgent.Sign(key, data)
}

func (a neverSignAgent) SignWithFlags(key ssh.PublicKey, data []byte, flags agent.SignatureFlags) (*ssh.Signature, error) {
	time.Sleep(10 * time.Minute)
	return a.ExtendedAgent.SignWithFlags(key, data, flags)
}

// TestCommitTouchTimeoutDegradesToUnsigned runs the real binary against an
// ssh-agent whose signature never arrives (a yubikey nobody touches), with a
// one-second touch budget. The run must not hang waiting for the touch: the
// commit signing times out, omc degrades to an unsigned commit (and an
// unsigned tag, since the signer is dropped), and exits 0 — the "always
// degrade, never block" contract applied to the smartcard wait.
func TestCommitTouchTimeoutDegradesToUnsigned(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping binary e2e in short mode")
	}
	bin := filepath.Join(t.TempDir(), "omc")
	build := exec.Command("go", "build", "-o", bin, ".")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}

	// An agent identity that never answers: signing blocks far beyond the
	// whole test, like an untouched security key.
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
	defer l.Close()
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				_ = agent.ServeAgent(neverSignAgent{ea}, c)
			}(c)
		}
	}()

	// The security-key handle: id_ed25519_sk plus a .pub matching the
	// agent identity (the harness key is a plain ed25519 stand-in).
	handle := filepath.Join(t.TempDir(), "id_ed25519_sk")
	pub, err := ssh.NewPublicKey(priv.Public())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(handle+".pub", ssh.MarshalAuthorizedKey(pub), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(handle, []byte("security key handle (device-bound), not a private key\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	// A fresh repo with one change to commit (git binaries are not needed;
	// omc itself opens, stages, commits and tags).
	repoDir := filepath.Join(t.TempDir(), "repo")
	repo, err := git.PlainInit(repoDir, false)
	if err != nil {
		t.Fatal(err)
	}
	_ = repo
	if err := os.WriteFile(filepath.Join(repoDir, "change.txt"), []byte("timeout\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command(bin)
	cmd.Dir = repoDir
	cmd.Env = append(os.Environ(),
		"SSH_AUTH_SOCK="+l.Addr().String(),
		"OMC_SIGN_KEY_PATH="+handle,
		"OMC_TOUCH_TIMEOUT=1",
		"OMC_SUBJECT=timeout degrade",
		"OMC_NAME=E2E User",
		"OMC_EMAIL=e2e@example.com",
		"OMC_TOUCH_NOTIFY=off",
	)
	start := time.Now()
	out, err := cmd.CombinedOutput()
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("omc must exit 0 even when the touch never comes: %v\n%s", err, out)
	}
	if elapsed > 15*time.Second {
		t.Fatalf("omc blocked %s waiting for a touch; run output:\n%s", elapsed, out)
	}
	got := string(out)
	for _, want := range []string{
		"waiting for security key touch",
		"touch timed out",
		"committing unsigned",
		"signed=false",
		"tagged",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("output missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "gpgsig") {
		t.Errorf("commit must be unsigned after the touch timeout")
	}

	// The commit and its tag must exist and be unsigned (no ssh signature
	// header on the commit object).
	checkRepo, err := git.PlainOpen(repoDir)
	if err != nil {
		t.Fatal(err)
	}
	head, err := checkRepo.Head()
	if err != nil {
		t.Fatal(err)
	}
	obj, err := checkRepo.CommitObject(head.Hash())
	if err != nil {
		t.Fatal(err)
	}
	if obj.PGPSignature != "" {
		t.Errorf("commit carries a signature after the touch timeout: %q", obj.PGPSignature)
	}
}
