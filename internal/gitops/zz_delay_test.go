package gitops

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/hiddeco/sshsig"
	"golang.org/x/crypto/ssh"

	"paepcke.de/omc/internal/sign"
)

// TestSignedTagDelayForcesTimestampSplit verifies that the signature in a
// signed tag matches the stored tag object even when the signing callback
// takes long enough to cross a second boundary. This catches the bug where
// tagPayload was called twice (once for signing, once for storage) with
// different time.Now() values.
func TestSignedTagDelayForcesTimestampSplit(t *testing.T) {
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	sshPriv, err := ssh.NewSignerFromKey(priv)
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
	h, err := Commit(repo, wt, CommitMessage{Subject: "s"})
	if err != nil {
		t.Fatal(err)
	}

	id := Identity{Name: "T", Email: "t@t"}
	ref, err := SignedTag(repo, h, "v0.0.1", "msg", id, func(payload []byte) ([]byte, error) {
		time.Sleep(1100 * time.Millisecond)
		sig, err := sshsig.Sign(strings.NewReader(string(payload)), sshPriv, sign.HashAlgorithm, sign.Namespace)
		if err != nil {
			return nil, err
		}
		return sshsig.Armor(sig), nil
	})
	if err != nil {
		t.Fatal(err)
	}

	obj, err := repo.TagObject(ref.Hash())
	if err != nil {
		t.Fatal(err)
	}

	var payload bytes.Buffer
	fmt.Fprintf(&payload, "object %s\n", obj.Target)
	fmt.Fprintf(&payload, "type %s\n", obj.TargetType.Bytes())
	fmt.Fprintf(&payload, "tag %s\n", obj.Name)
	payload.WriteString("tagger ")
	if err := obj.Tagger.Encode(&payload); err != nil {
		t.Fatal(err)
	}
	payload.WriteString("\n\n")
	msg := strings.TrimSpace(obj.Message)
	if msg == "" {
		msg = "update"
	}
	payload.WriteString(msg)
	payload.WriteString("\n")

	sigObj, err := sshsig.Unarmor([]byte(obj.PGPSignature))
	if err != nil {
		t.Fatal(err)
	}

	sshPub, err := ssh.NewPublicKey(priv.Public())
	if err != nil {
		t.Fatal(err)
	}
	if err := sshsig.Verify(
		bytes.NewReader(payload.Bytes()), sigObj,
		sshPub, sign.HashAlgorithm, sign.Namespace,
	); err != nil {
		t.Fatalf("signature does not match stored tag object (timestamp split bug): %v", err)
	}
}
