package main

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"

	"paepcke.de/omc/internal/config"
	"paepcke.de/omc/internal/output"
)

// TestLoadSignerSecurityKeySmokeWithAgent exercises the security-key
// signing path end to end with a real in-process agent: the device-backed
// key handle (id_ed25519_sk) is loaded as a software identity into an agent
// keyring, and loadSigner falls back to the sign.SecurityKeySigner agent
// route, producing a signer that can arm a signature.
func TestLoadSignerSecurityKeySmokeWithAgent(t *testing.T) {
	// A stand-in for the smartcard-held identity: the agent's view of an
	// sk key is a plain ed25519 key, which is exactly what x/crypto's
	// keyring can hold. The real device would enforce user presence.
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	keyring := agent.NewKeyring()
	if err := keyring.Add(agent.AddedKey{PrivateKey: priv, Comment: "sk stand-in"}); err != nil {
		t.Fatal(err)
	}
	// Serve the keyring over a unix socket like a real ssh-agent.
	sock := filepath.Join(t.TempDir(), "agent.sock")
	l, err := net.Listen("unix", sock)
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
				if err := agent.ServeAgent(keyring, c); err != nil && err.Error() != "EOF" {
					fmt.Fprintf(os.Stderr, "serve agent: %v\n", err)
				}
			}(c)
		}
	}()

	t.Setenv("SSH_AUTH_SOCK", sock)

	dir := t.TempDir()
	handle := filepath.Join(dir, "id_ed25519_sk")
	// The real id_ed25519_sk.pub contains the device's public half; the
	// agent holds the matching (smartcard-bound) private half. In this
	// harness the agent keyring holds a plain ed25519 key, so the .pub
	// file must carry that key's authorized line for HandleMatches to
	// line the handle up with the agent identity.
	sshPub, err := ssh.NewPublicKey(priv.Public())
	if err != nil {
		t.Fatal(err)
	}
	pubLine := ssh.MarshalAuthorizedKey(sshPub)
	if err := os.WriteFile(handle+".pub", pubLine, 0o600); err != nil {
		t.Fatal(err)
	}
	// The handle file itself is just a pointer document; give it an
	// unparseable sentinel so the .pub remains the source of identity.
	if err := os.WriteFile(handle, []byte("security key handle (device-bound), not a private key\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	ui := output.New(os.Stdout, os.Stderr)
	signer, sk := loadSigner(ui, config.Config{KeyPath: handle})
	if signer == nil {
		t.Fatalf("loadSigner returned nil signer for sk handle with agent")
	}
	if !sk {
		t.Errorf("loadSigner did not report smartcard mode for sk handle")
	}
	armored, err := signer.Sign([]byte("tree abc\nparent def\nauthor A <a@b> 0 +0000\n"))
	if err != nil {
		t.Fatalf("sign via agent-backed sk signer: %v", err)
	}
	if !bytes.Contains(armored, []byte("-----BEGIN SSH SIGNATURE-----")) {
		t.Errorf("missing armor header in %q", string(armored))
	}
	if signer.PublicAlgorithm() != "sk-ssh-ed25519@openssh.com" {
		t.Errorf("PublicAlgorithm() = %q, want sk-ssh-ed25519@openssh.com", signer.PublicAlgorithm())
	}
}
