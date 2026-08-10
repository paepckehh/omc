package gitops

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/go-git/go-git/v5"
	"github.com/hiddeco/sshsig"
	"golang.org/x/crypto/ssh"

	"paepcke.de/omc/internal/sign"
)

// TestSignedCommitViaSignLoad uses the real sign.Load path (the production
// key-loading code) and cryptographically verifies the stored signature
// against the headers-only payload, proving end-to-end correctness of the
// signing pipeline with a real key file on disk.
func TestSignedCommitViaSignLoad(t *testing.T) {
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	block, err := ssh.MarshalPrivateKey(priv, "")
	if err != nil {
		t.Fatal(err)
	}
	keyPath := filepath.Join(t.TempDir(), "id_ed25519")
	if err := os.WriteFile(keyPath, pem.EncodeToMemory(block), 0o600); err != nil {
		t.Fatal(err)
	}

	signer, err := sign.Load(keyPath)
	if err != nil {
		t.Fatal(err)
	}

	repo, wt, dir := initTestRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "x.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := wt.AddWithOptions(&git.AddOptions{All: true}); err != nil {
		t.Fatal(err)
	}
	h, err := SignedCommit(repo, wt, CommitMessage{Subject: "s"}, signer.Sign)
	if err != nil {
		t.Fatal(err)
	}

	obj, err := repo.CommitObject(h)
	if err != nil {
		t.Fatal(err)
	}
	if obj.PGPSignature == "" {
		t.Fatal("expected signature")
	}

	sshPub, err := ssh.NewPublicKey(priv.Public())
	if err != nil {
		t.Fatal(err)
	}

	var payload bytes.Buffer
	fmt.Fprintf(&payload, "tree %s\n", obj.TreeHash)
	for _, p := range obj.ParentHashes {
		fmt.Fprintf(&payload, "parent %s\n", p)
	}
	payload.WriteString("author ")
	if err := obj.Author.Encode(&payload); err != nil {
		t.Fatal(err)
	}
	payload.WriteString("\ncommitter ")
	if err := obj.Committer.Encode(&payload); err != nil {
		t.Fatal(err)
	}
	payload.WriteString("\n")

	sigObj, err := sshsig.Unarmor([]byte(obj.PGPSignature))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(sigObj.PublicKey.Marshal(), sshPub.Marshal()) {
		t.Fatal("signature was made by a different key than the one loaded")
	}
	if err := sshsig.Verify(
		bytes.NewReader(payload.Bytes()), sigObj,
		sshPub, sign.HashAlgorithm, sign.Namespace,
	); err != nil {
		t.Fatalf("verify: %v", err)
	}
}
