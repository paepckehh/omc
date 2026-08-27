package sign

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"
)

func writeKey(t *testing.T) string {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	block, err := ssh.MarshalPrivateKey(priv, "")
	if err != nil {
		t.Fatal(err)
	}
	marshaled := pem.EncodeToMemory(block)
	path := filepath.Join(t.TempDir(), "id_ed25519")
	if err := os.WriteFile(path, marshaled, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadAndSign(t *testing.T) {
	path := writeKey(t)
	s, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	payload := []byte("tree abc\nparent def\nauthor A <a@b> 0 +0000\n\nsubject\n")
	sig, err := s.Sign(payload)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	if !strings.Contains(string(sig), "-----BEGIN SSH SIGNATURE-----") {
		t.Errorf("signature missing armor header: %q", string(sig))
	}
	if !strings.Contains(string(sig), "-----END SSH SIGNATURE-----") {
		t.Errorf("signature missing armor footer: %q", string(sig))
	}
}

func TestLoadInvalidKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad")
	if err := os.WriteFile(path, []byte("not a key"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("expected error for invalid key")
	}
}

func TestLoadMissingFile(t *testing.T) {
	if _, err := Load("/nonexistent/key"); err == nil {
		t.Fatal("expected error for missing key file")
	}
}

func TestIsSecurityKeyPath(t *testing.T) {
	cases := []struct {
		path string
		want bool
	}{
		{"~/.ssh/id_ed25519_sk", true},
		{"/home/me/.ssh/id_ed25519_sk", true},
		{"id_ed25519_sk", true},
		{"id_ecdsa_sk", true},
		{"id_ed25519_sk_rk", true},
		{"id_ed25519_sk.pub", true},
		{"/home/me/.ssh/id_rsa", false},
		{"/home/me/.ssh/id_ed25519", false},
		{"", false},
	}
	for _, c := range cases {
		if got := IsSecurityKeyPath(c.path); got != c.want {
			t.Errorf("IsSecurityKeyPath(%q) = %v, want %v", c.path, got, c.want)
		}
	}
}

func TestDetectKind(t *testing.T) {
	// Software key -> KindSoftware.
	soft := writeKey(t)
	if k := DetectKind(soft); k != KindSoftware {
		t.Errorf("DetectKind(software) = %v, want KindSoftware", k)
	}

	// Security-key handle: conventional filename, plus a .pub file with an
	// sk-* public key line next to it -> KindSecurityKey.
	dir := t.TempDir()
	handle := filepath.Join(dir, "id_ed25519_sk")
	if err := os.WriteFile(handle, []byte("sk-ssh-ed25519@openssh.com AAAAGnNrLXNzaC1lZDI1NTE5QG9wZW5zc2guY29tAAAAIAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAABHNzaDo= test@device\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if k := DetectKind(handle); k != KindSecurityKey {
		t.Errorf("DetectKind(sk handle) = %v, want KindSecurityKey", k)
	}

	// Broken / missing -> KindBroken.
	if k := DetectKind(filepath.Join(dir, "missing")); k != KindBroken {
		t.Errorf("DetectKind(missing) = %v, want KindBroken", k)
	}
	if err := os.WriteFile(filepath.Join(dir, "junk"), []byte("not a key"), 0o600); err != nil {
		t.Fatal(err)
	}
	if k := DetectKind(filepath.Join(dir, "junk")); k != KindBroken {
		t.Errorf("DetectKind(junk) = %v, want KindBroken", k)
	}
}

func TestHandleMatches(t *testing.T) {
	pub, _, _, _, err := ssh.ParseAuthorizedKey([]byte("sk-ssh-ed25519@openssh.com AAAAGnNrLXNzaC1lZDI1NTE5QG9wZW5zc2guY29tAAAAIAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAABHNzaDo= test@device\n"))
	if err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	handle := filepath.Join(dir, "id_ed25519_sk")
	if err := os.WriteFile(handle,
		[]byte("-----BEGIN OPENSSH PRIVATE KEY-----\nthis is just a key handle, not a real private key\n-----END OPENSSH PRIVATE KEY-----\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	// No parseable public half in the handle file, but the conventional
	// id_ed25519_sk name is the fallback signal -> matches.
	if !HandleMatches(pub, handle) {
		t.Errorf("HandleMatches(sk handle file) = false, want true (name fallback)")
	}
	if HandleMatches(pub, "id_rsa") {
		t.Errorf("HandleMatches(rsa name) = true, want false")
	}
}
