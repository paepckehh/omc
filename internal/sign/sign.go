// Package sign implements SSH signing of git commit objects, the exact
// equivalent of "git commit -S" with an SSH key (git's ssh format). The
// signature is produced in pure Go via hiddeco/sshsig, so no ssh-keygen
// binary is required at runtime.
//
// Security-key (FIDO2) keys created with "ssh-keygen -t ed25519-sk" or
// "-t ecdsa-sk" are supported too, but only through the ssh-agent: the
// private key material lives on the smartcard and never leaves the device,
// so the "private key file" on disk is only a public half plus a key
// handle. Signing those requires the agent to forward the challenge to the
// authenticator (which enforces the touch/pin check). Everything stays pure
// Go; no external binaries are invoked.
package sign

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"

	"github.com/hiddeco/sshsig"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
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
	// psuedoKeyType overrides the algorithm reported by PublicAlgorithm
	// when the signer delegates to the ssh-agent: the agent presents a
	// security-key identity as a plain ed25519/ecdsa key (it strips the
	// sk-* type), but the produced signature is an sk signature, so the
	// algorithm string must reflect the sk-* type for git/devices.
	psuedoKeyType string
}

// Kind classifies what lives at a key path, so callers can pick the right
// signing backend before attempting to load it.
type Kind int

const (
	// KindBroken is a path that is unreadable or contains neither a usable
	// SSH private key nor a recognizable security-key handle.
	KindBroken Kind = iota
	// KindSoftware is a normal software SSH private key, loadable in pure
	// Go and directly signable.
	KindSoftware
	// KindSecurityKey is a FIDO2 security-key handle (e.g.
	// ~/.ssh/id_ed25519_sk): the file is only a pointer to a smartcard-held
	// key, so signing must go through the ssh-agent.
	KindSecurityKey
)

// ErrSecurityKeyOnly is returned by Load when pointed at the private half
// of a FIDO2 security-key pair (id_ed25519_sk) instead of a software key.
// The key material is not in the file, so pure Go cannot sign with it;
// callers should use SecurityKeySigner (via the agent) or degrade.
var ErrSecurityKeyOnly = errors.New("security key (FIDO2) private key cannot be used without the ssh-agent")

// securityKeyNames are the conventional file names of FIDO2 security-key
// key pairs created by "ssh-keygen -t ed25519-sk" and "-t ecdsa-sk". The
// _rk variants are used when a resident/discoverable credential was
// requested. Names are matched after the |_sk| prefix, so platform-specific
// suffixes (e.g. "id_ed25519_sk_rk") are honored too.
var securityKeyNames = []string{
	"id_ed25519_sk", "id_ecdsa_sk",
	"id_ed25519_sk_rk", "id_ecdsa_sk_rk",
}

// IsSecurityKeyPath reports whether a key path names a FIDO2 security-key
// key handle. The check is a full-path or basename match against the
// conventional id_ed25519_sk / id_ecdsa_sk names (with optional suffix).
// Detection is name-based because the file carries no type tag that pure Go
// can inspect: it is not a parseable private key (there is no private half),
// and the public half inside can only be read from the matching .pub file.
func IsSecurityKeyPath(path string) bool {
	if path == "" {
		return false
	}
	for _, n := range securityKeyNames {
		if path == n {
			return true
		}
	}
	base := filepath.Base(path)
	for _, n := range securityKeyNames {
		if base == n || strings.HasPrefix(base, n+".") || strings.HasPrefix(base, n+"_") {
			return true
		}
	}
	return false
}

// DetectKind reads the file at path and reports what kind of key it is.
// A file that parses as a normal SSH private key is KindSoftware; a file
// that parses as a security-key public/authorized line, or that carries one
// of the conventional sk file names, is KindSecurityKey; everything else is
// KindBroken. The check never fails the pipeline: it is only a hint for
// which loading path to attempt.
func DetectKind(path string) Kind {
	data, err := os.ReadFile(path)
	if err != nil {
		return KindBroken
	}
	// A software private key parses directly.
	if priv, err := ssh.ParsePrivateKey(data); err == nil {
		if isSecurityKey(priv.PublicKey()) {
			// Unreachable today: x/crypto cannot parse an sk
			// private envelope, but be safe rather than sorry.
			return KindSecurityKey
		}
		return KindSoftware
	}
	// The id_*_sk file is not a private key, but ssh-keygen also stores
	// the public half in the matching ".pub" file next to it; try that.
	if pub, err := parseAuthorizedKeyAt(path + ".pub"); err == nil {
		if isSecurityKey(pub) {
			return KindSecurityKey
		}
	}
	if IsSecurityKeyPath(path) {
		return KindSecurityKey
	}
	return KindBroken
}

// parseAuthorizedKeyAt parses the first authorized-keys line in the file at
// path, returning its public key.
func parseAuthorizedKeyAt(path string) (ssh.PublicKey, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	pub, _, _, _, err := ssh.ParseAuthorizedKey(data)
	if err != nil {
		return nil, err
	}
	return pub, nil
}

// Load reads and parses the SSH private key at path. Hardware-backed
// (security-key / FIDO2) keys yield ErrSecurityKeyOnly: the file on disk
// contains no private key material, so it cannot be used directly in pure
// Go. Use SecurityKeySigner for those keys, or degrade to an unsigned
// commit, exactly like for any other unusable key.
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
	if isSecurityKey(signer.PublicKey()) {
		return nil, fmt.Errorf("%w: %s stays on the smartcard", ErrSecurityKeyOnly, path)
	}
	return &Signer{signer: signer}, nil
}

// SecurityKeySigner returns a Signer backed by the ssh-agent for the FIDO2
// security-key identity named by keyPath (e.g. ~/.ssh/id_ed25519_sk),
// mirroring how git resolves a security-key signing key. Every signature is
// delegated to the agent, which forwards the challenge to the smartcard;
// the user-presence (touch) check is enforced by the authenticator, not by
// omc. It returns an error when the agent is unreachable or does not hold
// the identity (e.g. the key was never ssh-add'ed); callers degrade to a
// warning exactly like for an unusable key file.
func SecurityKeySigner(keyPath string) (*Signer, error) {
	conn, err := net.Dial("unix", os.Getenv("SSH_AUTH_SOCK"))
	if err != nil {
		return nil, fmt.Errorf("connect to ssh-agent for security key %s (is ssh-agent running?): %w", keyPath, err)
	}
	ag := agent.NewClient(conn)
	signers, err := ag.Signers()
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("list agent identities for security key %s: %w", keyPath, err)
	}

	var found ssh.Signer
	for _, s := range signers {
		if HandleMatches(s.PublicKey(), keyPath) {
			found = s
			break
		}
	}
	if found == nil {
		conn.Close()
		return nil, fmt.Errorf("ssh-agent does not hold security key %s (add it with: ssh-add %s)", filepath.Base(keyPath), keyPath)
	}

	return &Signer{signer: &agentSigner{signer: found, conn: conn}, psuedoKeyType: skTypeOf(found)}, nil
}

// skTypeOf returns the sk-* algorithm an agent-backed security-key signer
// produces signatures under. The agent reports the identity's public key as
// the plain underlying type; OpenSSH maps those to the sk-* signature
// algorithm, so we mirror that here.
func skTypeOf(s ssh.Signer) string {
	if s == nil {
		return ""
	}
	switch s.PublicKey().Type() {
	case ssh.KeyAlgoED25519:
		return ssh.KeyAlgoSKED25519
	case ssh.KeyAlgoECDSA256:
		return ssh.KeyAlgoSKECDSA256
	}
	return ""
}

// agentSigner is an ssh.Signer that forwards Sign calls to an agent-held
// identity and keeps the agent connection open for the signer's lifetime.
// The agent-side security-key signer reports its public key with the sk-*
// type, so signatures (and the recorded algorithm) stay byte-compatible
// with what "git commit -S" / "git tag -s" produce on a smartcard.
type agentSigner struct {
	signer ssh.Signer
	// conn is retained so the agent client's request pipeline stays
	// alive; it is deliberately never closed here, the short-lived CLI
	// exits right after signing and the OS cleans up.
	conn net.Conn
}

func (s *agentSigner) PublicKey() ssh.PublicKey { return s.signer.PublicKey() }

func (s *agentSigner) Sign(rand io.Reader, data []byte) (*ssh.Signature, error) {
	return s.signer.Sign(rand, data)
}

var _ ssh.Signer = (*agentSigner)(nil)

// HandleMatches reports whether the given public key corresponds to the
// security-key handle stored at keyPath. It parses path (falling back to
// path + ".pub") as an authorized-keys line and compares the blob to pub.
// When neither parses, the conventional file name is the only signal left.
func HandleMatches(pub ssh.PublicKey, keyPath string) bool {
	for _, p := range []string{keyPath, keyPath + ".pub"} {
		handlePub, err := parseAuthorizedKeyAt(p)
		if err != nil {
			continue
		}
		return bytes.Equal(handlePub.Marshal(), pub.Marshal())
	}
	return IsSecurityKeyPath(keyPath)
}

// isSecurityKey reports whether pub is a FIDO2 security-key public key
// (sk-ssh-ed25519@openssh.com or sk-ecdsa-sha2-nistp256@openssh.com).
func isSecurityKey(pub ssh.PublicKey) bool {
	switch pub.Type() {
	case ssh.KeyAlgoSKECDSA256, ssh.KeyAlgoSKED25519:
		return true
	default:
		return false
	}
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

// PublicAlgorithm returns the SSH signature algorithm this signer uses
// (e.g. "rsa-sha2-512" for RSA, "sk-ssh-ed25519@openssh.com" for a
// security-key key). It is exposed so the CLI can state exactly which
// algorithm a signing notice refers to.
func (s *Signer) PublicAlgorithm() string {
	if s.psuedoKeyType != "" {
		return s.psuedoKeyType
	}
	switch s.signer.PublicKey().Type() {
	case ssh.KeyAlgoRSA:
		return ssh.KeyAlgoRSASHA512
	default:
		return s.signer.PublicKey().Type()
	}
}
