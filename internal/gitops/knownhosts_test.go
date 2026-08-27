package gitops

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"

	gogitssh "github.com/go-git/go-git/v5/plumbing/transport/ssh"
	"golang.org/x/crypto/ssh"
)

// writeKnownHosts writes a single host-key line into a temp known_hosts file
// and points SSH_KNOWN_HOSTS at it, so applyKnownHosts reads only that file
// regardless of the host's real ~/.ssh or /etc/ssh contents.
func writeKnownHosts(t *testing.T, host, line string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "known_hosts")
	if err := os.WriteFile(path, []byte(host+" "+line+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SSH_KNOWN_HOSTS", path)
	return path
}

// fakeHostKey builds a valid known_hosts key field ("ssh-ed25519 <base64>")
// from a freshly generated ed25519 key, without depending on any real server.
func fakeHostKey(t *testing.T) string {
	t.Helper()
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	sshPub, err := ssh.NewPublicKey(pub)
	if err != nil {
		t.Fatal(err)
	}
	return sshPub.Type() + " " + base64.StdEncoding.EncodeToString(sshPub.Marshal())
}

// TestApplyKnownHostsRestrictsAlgorithms verifies the core fix: when the
// target host is present in known_hosts with an ed25519 key, the auth
// method's HostKeyAlgorithms is restricted to exactly the algorithms stored
// for that host (here ssh-ed25519). Without this, x/crypto/ssh would use its
// full default list (ecdsa before ed25519) and a host like github.com would
// present an ecdsa key the known_hosts file does not store, failing the
// handshake with "knownhosts: key mismatch".
func TestApplyKnownHostsRestrictsAlgorithms(t *testing.T) {
	writeKnownHosts(t, "github.com", fakeHostKey(t))

	auth := &gogitssh.PublicKeysCallback{
		User: "git",
		Callback: func() ([]ssh.Signer, error) {
			return nil, nil // not reached during host-key verification
		},
	}
	if err := applyKnownHosts(auth, "github.com:22"); err != nil {
		t.Fatalf("applyKnownHosts: %v", err)
	}
	if auth.HostKeyCallback == nil {
		t.Fatal("HostKeyCallback not set")
	}
	if len(auth.HostKeyAlgorithms) != 1 || auth.HostKeyAlgorithms[0] != "ssh-ed25519" {
		t.Errorf("HostKeyAlgorithms = %v, want [ssh-ed25519]", auth.HostKeyAlgorithms)
	}
}

// TestApplyKnownHostsUnknownHostLeavesAlgorithmsUnset confirms that a host
// not present in known_hosts keeps HostKeyAlgorithms unset (nil) so that
// x/crypto/ssh falls back to its defaults and the known_hosts callback
// itself surfaces the "key is unknown" failure, preserving the never-trust
// contract rather than offering an empty algorithm set.
func TestApplyKnownHostsUnknownHostLeavesAlgorithmsUnset(t *testing.T) {
	writeKnownHosts(t, "github.com", fakeHostKey(t))

	auth := &gogitssh.PublicKeysCallback{
		User:     "git",
		Callback: func() ([]ssh.Signer, error) { return nil, nil },
	}
	if err := applyKnownHosts(auth, "example.org:22"); err != nil {
		t.Fatalf("applyKnownHosts: %v", err)
	}
	if auth.HostKeyCallback == nil {
		t.Fatal("HostKeyCallback not set")
	}
	if len(auth.HostKeyAlgorithms) != 0 {
		t.Errorf("HostKeyAlgorithms = %v, want empty for unknown host", auth.HostKeyAlgorithms)
	}
}

// TestApplyKnownHostsPublicKeys confirms applyKnownHosts also wires the
// fields for the *PublicKeys auth method (file-key path), not just the
// agent/security-key callback variant.
func TestApplyKnownHostsPublicKeys(t *testing.T) {
	writeKnownHosts(t, "github.com", fakeHostKey(t))

	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatalf("NewSignerFromKey: %v", err)
	}
	auth := &gogitssh.PublicKeys{User: "git", Signer: signer}
	if err := applyKnownHosts(auth, "github.com:22"); err != nil {
		t.Fatalf("applyKnownHosts: %v", err)
	}
	if auth.HostKeyCallback == nil {
		t.Fatal("HostKeyCallback not set on *PublicKeys")
	}
	if len(auth.HostKeyAlgorithms) != 1 || auth.HostKeyAlgorithms[0] != "ssh-ed25519" {
		t.Errorf("HostKeyAlgorithms = %v, want [ssh-ed25519]", auth.HostKeyAlgorithms)
	}
}
