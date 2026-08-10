package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/crypto/ssh"

	"paepcke.de/omc/internal/config"
	"paepcke.de/omc/internal/output"
)

// writeSoftwareKey writes a fresh OpenSSH-format ed25519 private key to
// path and returns it. Used to exercise the plain software-key load path.
func writeSoftwareKey(t *testing.T, path string) {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	block, err := ssh.MarshalPrivateKey(priv, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, pem.EncodeToMemory(block), 0o600); err != nil {
		t.Fatal(err)
	}
}

// TestLoadSignerSecurityKeyUsesAgent probes the FIDO2 security-key degrade
// path of loadSigner: an id_ed25519_sk handle cannot be loaded in pure Go,
// so loadSigner must attempt the ssh-agent route. With no agent present this
// resolves to an explicit warning and a nil signer (unsigned commit), never
// a panic or a broken signature.
func TestLoadSignerSecurityKeyNoAgent(t *testing.T) {
	t.Setenv("SSH_AUTH_SOCK", filepath.Join(t.TempDir(), "nonexistent.sock"))

	dir := t.TempDir()
	handle := filepath.Join(dir, "id_ed25519_sk")
	if err := os.WriteFile(handle, []byte("sk-ssh-ed25519@openssh.com AAAAGnNrLXNzaC1lZDI1NTE5QG9wZW5zc2guY29tAAAAIAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAABHNzaDo= test@device\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	ui := output.New(os.Stdout, os.Stderr)
	signer, _ := loadSigner(ui, config.Config{KeyPath: handle})
	if signer != nil {
		t.Fatalf("loadSigner returned a signer for an sk handle without an agent; want nil (degrade to unsigned)")
	}
}

// TestLoadSignerUnusableKey covers the plain unusable-key path: a corrupt
// file that is not an sk handle must warn and resolve to a nil signer.
func TestLoadSignerUnusableKey(t *testing.T) {
	dir := t.TempDir()
	bad := filepath.Join(dir, "id_rsa")
	if err := os.WriteFile(bad, []byte("not a key"), 0o600); err != nil {
		t.Fatal(err)
	}

	ui := output.New(os.Stdout, os.Stderr)
	signer, _ := loadSigner(ui, config.Config{KeyPath: bad})
	if signer != nil {
		t.Fatalf("loadSigner returned a signer for corrupt key; want nil")
	}
}

// TestLoadSignerSoftwareKey verifies the happy path still loads a normal
// software key directly.
func TestLoadSignerSoftwareKey(t *testing.T) {
	dir := t.TempDir()
	key := filepath.Join(dir, "id_ed25519")
	writeSoftwareKey(t, key)

	ui := output.New(os.Stdout, os.Stderr)
	signer, _ := loadSigner(ui, config.Config{KeyPath: key})
	if signer == nil {
		t.Fatalf("loadSigner returned nil for a valid software key")
	}
	if signer.PublicAlgorithm() == "" {
		t.Fatalf("loadSigner signer has empty public algorithm")
	}
}
