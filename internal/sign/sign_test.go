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
