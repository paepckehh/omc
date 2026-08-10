// Package sign implements SSH signing of git commit objects, the exact
// equivalent of "git commit -S" with an SSH key (git's ssh format). The
// signature is produced in pure Go via hiddeco/sshsig, so no ssh-keygen
// binary is required at runtime.
package sign

import (
	"bytes"
	"fmt"
	"os"

	"github.com/hiddeco/sshsig"
	"golang.org/x/crypto/ssh"
)

const (
	// Namespace is the SSH signature namespace used by git for commits.
	Namespace = "git"
	// HashAlgorithm mirrors git's default for SSH signatures.
	HashAlgorithm = sshsig.HashSHA512
)

// Signer loads and holds an SSH private key and produces armored SSH
// signatures over arbitrary payloads.
type Signer struct {
	signer ssh.Signer
}

// Load reads and parses the SSH private key at path.
func Load(path string) (*Signer, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read key %s: %w", path, err)
	}
	// Attempt plain first; fall back to passphrase-protected prompts is
	// intentionally not supported (no interactive I/O).
	signer, err := ssh.ParsePrivateKey(data)
	if err != nil {
		return nil, fmt.Errorf("parse key %s: %w", path, err)
	}
	return &Signer{signer: signer}, nil
}

// Sign returns the armored SSH signature over data using the git namespace
// and SHA-512, byte-for-byte what "git commit -S" produces.
func (s *Signer) Sign(data []byte) ([]byte, error) {
	sig, err := sshsig.Sign(bytes.NewReader(data), s.signer, HashAlgorithm, Namespace)
	if err != nil {
		return nil, fmt.Errorf("ssh sign: %w", err)
	}
	return sshsig.Armor(sig), nil
}
