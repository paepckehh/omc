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

	"github.com/go-git/go-git/v5"
	"github.com/hiddeco/sshsig"
	"golang.org/x/crypto/ssh"

	"paepcke.de/ocommit/internal/sign"
)

// initTestRepo creates a fresh git repository in a temp dir with a configured
// identity, chdirs there and returns the repo and worktree.
func initTestRepo(t *testing.T) (*git.Repository, *git.Worktree, string) {
	t.Helper()
	dir := t.TempDir()
	repo, err := git.PlainInit(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	// Configure a deterministic identity so commits have a known author.
	t.Setenv("GIT_AUTHOR_NAME", "Test User")
	t.Setenv("GIT_AUTHOR_EMAIL", "test@example.com")
	t.Setenv("GIT_COMMITTER_NAME", "Test User")
	t.Setenv("GIT_COMMITTER_EMAIL", "test@example.com")

	wt, err := repo.Worktree()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	return repo, wt, dir
}

func commitFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestCommitCreateWithParent(t *testing.T) {
	repo, wt, dir := initTestRepo(t)
	commitFile(t, dir, "a.txt", "hello")
	if err := wt.AddWithOptions(&git.AddOptions{All: true}); err != nil {
		t.Fatal(err)
	}
	root, err := Commit(repo, wt, CommitMessage{Subject: "initial"})
	if err != nil {
		t.Fatal(err)
	}
	if root.IsZero() {
		t.Fatal("zero hash")
	}

	commitFile(t, dir, "a.txt", "changed\n")
	commitFile(t, dir, "b.txt", "new\n")
	if err := wt.AddWithOptions(&git.AddOptions{All: true}); err != nil {
		t.Fatal(err)
	}
	second, err := Commit(repo, wt, CommitMessage{Subject: "second", Body: "details"})
	if err != nil {
		t.Fatal(err)
	}
	head, err := repo.Head()
	if err != nil {
		t.Fatal(err)
	}
	if head.Hash() != second {
		t.Errorf("HEAD = %s, want %s", head.Hash(), second)
	}

	obj, err := repo.CommitObject(second)
	if err != nil {
		t.Fatal(err)
	}
	if len(obj.ParentHashes) != 1 || obj.ParentHashes[0] != root {
		t.Errorf("parents = %v, want [%s]", obj.ParentHashes, root)
	}
}

func TestCommitRootNoParent(t *testing.T) {
	repo, wt, dir := initTestRepo(t)
	commitFile(t, dir, "x.txt", "x")
	if err := wt.AddWithOptions(&git.AddOptions{All: true}); err != nil {
		t.Fatal(err)
	}
	h, err := Commit(repo, wt, CommitMessage{Subject: "first"})
	if err != nil {
		t.Fatal(err)
	}
	c, err := repo.CommitObject(h)
	if err != nil {
		t.Fatal(err)
	}
	if len(c.ParentHashes) != 0 {
		t.Errorf("root commit has parents: %v", c.ParentHashes)
	}
}

func TestOpenFindParentRepo(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "sub", "dir")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := git.PlainInit(dir, false); err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(sub); err != nil {
		t.Fatal(err)
	}
	_, _, err := Open()
	if err != nil {
		t.Fatalf("Open() from subdir: %v", err)
	}
}

func TestOpenOutsideRepo(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Open(); err != ErrNotARepository {
		t.Fatalf("got %v, want ErrNotARepository", err)
	}
}

func TestStagedDiff(t *testing.T) {
	repo, wt, dir := initTestRepo(t)
	commitFile(t, dir, "hello.txt", "hello\n")
	if err := wt.AddWithOptions(&git.AddOptions{All: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := Commit(repo, wt, CommitMessage{Subject: "initial"}); err != nil {
		t.Fatal(err)
	}

	commitFile(t, dir, "hello.txt", "hello world\n")
	if err := wt.AddWithOptions(&git.AddOptions{All: true}); err != nil {
		t.Fatal(err)
	}
	diff, files, err := StagedDiff(repo, wt)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(diff, "hello.txt") {
		t.Errorf("diff missing file name: %q", diff)
	}
	if !strings.Contains(diff, "+hello world") {
		t.Errorf("diff missing added line: %q", diff)
	}
	if len(files) == 0 {
		t.Errorf("expected at least one changed file, got %v", files)
	} else if files[0] != "hello.txt" {
		t.Errorf("files[0] = %q, want hello.txt", files[0])
	}
}

func TestLogFormat(t *testing.T) {
	repo, wt, dir := initTestRepo(t)
	commitFile(t, dir, "a.txt", "a")
	if err := wt.AddWithOptions(&git.AddOptions{All: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := Commit(repo, wt, CommitMessage{Subject: "First commit"}); err != nil {
		t.Fatal(err)
	}

	out, err := Log(repo, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "First commit") {
		t.Errorf("log missing subject: %q", out)
	}
}

func TestResolveIdentityPriority(t *testing.T) {
	dir := t.TempDir()
	repo, err := git.PlainInit(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	cfg, err := repo.Config()
	if err != nil {
		t.Fatal(err)
	}
	cfg.User.Name = "Git Config User"
	cfg.User.Email = "config@example.com"
	if err := repo.SetConfig(cfg); err != nil {
		t.Fatal(err)
	}

	t.Run("oconfig env wins", func(t *testing.T) {
		t.Setenv("OCOMMIT_NAME", "Env User")
		t.Setenv("OCOMMIT_EMAIL", "env@example.com")
		t.Setenv("GIT_AUTHOR_NAME", "")
		t.Setenv("GIT_AUTHOR_EMAIL", "")
		t.Setenv("GIT_COMMITTER_NAME", "")
		t.Setenv("GIT_COMMITTER_EMAIL", "")
		id := ResolveIdentity(repo)
		if id.Name != "Env User" || id.Email != "env@example.com" {
			t.Errorf("got %+v, want env identity", id)
		}
	})

	t.Run("git author env wins over config", func(t *testing.T) {
		t.Setenv("OCOMMIT_NAME", "")
		t.Setenv("OCOMMIT_EMAIL", "")
		t.Setenv("GIT_AUTHOR_NAME", "Git Author")
		t.Setenv("GIT_AUTHOR_EMAIL", "author@example.com")
		id := ResolveIdentity(repo)
		if id.Name != "Git Author" || id.Email != "author@example.com" {
			t.Errorf("got %+v, want git author identity", id)
		}
	})

	t.Run("repo config fallback", func(t *testing.T) {
		t.Setenv("OCOMMIT_NAME", "")
		t.Setenv("OCOMMIT_EMAIL", "")
		t.Setenv("GIT_AUTHOR_NAME", "")
		t.Setenv("GIT_AUTHOR_EMAIL", "")
		t.Setenv("GIT_COMMITTER_NAME", "")
		t.Setenv("GIT_COMMITTER_EMAIL", "")
		id := ResolveIdentity(repo)
		if id.Name != "Git Config User" || id.Email != "config@example.com" {
			t.Errorf("got %+v, want repo config identity", id)
		}
	})

	t.Run("defaults without config", func(t *testing.T) {
		t.Setenv("OCOMMIT_NAME", "")
		t.Setenv("OCOMMIT_EMAIL", "")
		t.Setenv("GIT_AUTHOR_NAME", "")
		t.Setenv("GIT_AUTHOR_EMAIL", "")
		t.Setenv("GIT_COMMITTER_NAME", "")
		t.Setenv("GIT_COMMITTER_EMAIL", "")
		id := ResolveIdentity(nil)
		if id.Name != DefaultName || id.Email != DefaultEmail {
			t.Errorf("got %+v, want defaults %q <%s>", id, DefaultName, DefaultEmail)
		}
	})
}

// TestSignedCommitRoundTrip signs a commit and verifies the SSH signature
// programmatically against the payload (commit without signature header).
func TestSignedCommitRoundTrip(t *testing.T) {
	repo, wt, dir := initTestRepo(t)
	commitFile(t, dir, "signed.txt", "signed content\n")
	if err := wt.AddWithOptions(&git.AddOptions{All: true}); err != nil {
		t.Fatal(err)
	}

	_, privEd, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	sshPriv, err := ssh.NewSignerFromKey(privEd)
	if err != nil {
		t.Fatal(err)
	}

	h, err := SignedCommit(repo, wt, CommitMessage{Subject: "signed"}, func(payload []byte) ([]byte, error) {
		sig, err := sshsig.Sign(strings.NewReader(string(payload)), sshPriv, sign.HashAlgorithm, sign.Namespace)
		if err != nil {
			return nil, err
		}
		return sshsig.Armor(sig), nil
	})
	if err != nil {
		t.Fatal(err)
	}

	obj, err := repo.CommitObject(h)
	if err != nil {
		t.Fatal(err)
	}
	if obj.PGPSignature == "" {
		t.Fatal("expected PGPSignature set")
	}
	if !strings.Contains(obj.PGPSignature, "BEGIN SSH SIGNATURE") {
		t.Errorf("signature not armored: %q", obj.PGPSignature)
	}

	// Payload for verification = exactly what SignedCommit signed: the
	// headers-only block (tree, parents, author, committer), no message.
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

	sig, err := sshsig.Unarmor([]byte(obj.PGPSignature))
	if err != nil {
		t.Fatal(err)
	}
	if err := sshsig.Verify(
		strings.NewReader(payload.String()), sig,
		sshPriv.PublicKey(), sign.HashAlgorithm, sign.Namespace,
	); err != nil {
		t.Fatalf("verify signature: %v", err)
	}
}

// --- semver tag tests --------------------------------------------------------

func TestNextSemverTag(t *testing.T) {
	cases := []struct {
		latest, want string
	}{
		{"", "v0.0.1"},
		{"v0.0.1", "v0.0.2"},
		{"v1.2.3", "v1.2.4"},
		{"v10.20.30", "v10.20.31"},
		{"v0.1.0", "v0.1.1"},
		{"v2.0.0", "v2.0.1"},
		{"not-a-tag", "v0.0.1"},
		{"v1.2.3-rc.1", "v1.2.4"},
		{"1.2.3", "v1.2.4"},
	}
	for _, c := range cases {
		got := NextSemverTag(c.latest)
		if got != c.want {
			t.Errorf("NextSemverTag(%q) = %q, want %q", c.latest, got, c.want)
		}
	}
}

func TestLatestSemverTagEmpty(t *testing.T) {
	repo, _, _ := initTestRepo(t)
	got, err := LatestSemverTag(repo)
	if err != nil {
		t.Fatalf("LatestSemverTag: %v", err)
	}
	if got != "" {
		t.Errorf("got %q, want empty (no tags in fresh repo)", got)
	}
}

func TestLatestSemverTagFindsHighest(t *testing.T) {
	repo, wt, dir := initTestRepo(t)
	commitFile(t, dir, "a.txt", "a")
	if err := wt.AddWithOptions(&git.AddOptions{All: true}); err != nil {
		t.Fatal(err)
	}
	h, err := Commit(repo, wt, CommitMessage{Subject: "init"})
	if err != nil {
		t.Fatal(err)
	}
	id := Identity{Name: "Tagger", Email: "tag@example.com"}
	for _, name := range []string{"v0.1.0", "v0.0.5", "v1.0.0", "v0.10.0", "notsemver", "v0.1.0-rc.1"} {
		if _, err := CreateTag(repo, h, name, "msg", id); err != nil {
			t.Fatalf("CreateTag(%s): %v", name, err)
		}
	}
	got, err := LatestSemverTag(repo)
	if err != nil {
		t.Fatalf("LatestSemverTag: %v", err)
	}
	if got != "v1.0.0" {
		t.Errorf("got %q, want v1.0.0", got)
	}
}

func TestCreateTagAnnotated(t *testing.T) {
	repo, wt, dir := initTestRepo(t)
	commitFile(t, dir, "a.txt", "a")
	if err := wt.AddWithOptions(&git.AddOptions{All: true}); err != nil {
		t.Fatal(err)
	}
	h, err := Commit(repo, wt, CommitMessage{Subject: "init"})
	if err != nil {
		t.Fatal(err)
	}
	id := Identity{Name: "Tagger", Email: "tag@example.com"}
	ref, err := CreateTag(repo, h, "v0.0.1", "release one", id)
	if err != nil {
		t.Fatalf("CreateTag: %v", err)
	}
	if ref == nil {
		t.Fatal("nil ref returned")
	}

	obj, err := repo.TagObject(ref.Hash())
	if err != nil {
		t.Fatalf("TagObject: %v", err)
	}
	if obj.Name != "v0.0.1" {
		t.Errorf("name = %q, want v0.0.1", obj.Name)
	}
	if obj.Target != h {
		t.Errorf("target = %s, want %s", obj.Target, h)
	}
	if obj.Message != "release one\n" {
		t.Errorf("message = %q, want %q", obj.Message, "release one\n")
	}
	if obj.Tagger.Name != "Tagger" {
		t.Errorf("tagger name = %q, want Tagger", obj.Tagger.Name)
	}
	if obj.PGPSignature != "" {
		t.Errorf("unsigned tag has PGPSignature: %q", obj.PGPSignature)
	}
}

func TestCreateTagDuplicateFails(t *testing.T) {
	repo, wt, dir := initTestRepo(t)
	commitFile(t, dir, "a.txt", "a")
	if err := wt.AddWithOptions(&git.AddOptions{All: true}); err != nil {
		t.Fatal(err)
	}
	h, err := Commit(repo, wt, CommitMessage{Subject: "init"})
	if err != nil {
		t.Fatal(err)
	}
	id := Identity{Name: "Tagger", Email: "tag@example.com"}
	if _, err := CreateTag(repo, h, "v0.0.1", "first", id); err != nil {
		t.Fatalf("CreateTag first: %v", err)
	}
	if _, err := CreateTag(repo, h, "v0.0.1", "second", id); err == nil {
		t.Fatal("expected error for duplicate tag, got nil")
	}
}

func TestSignedTagRoundTrip(t *testing.T) {
	repo, wt, dir := initTestRepo(t)
	commitFile(t, dir, "a.txt", "a")
	if err := wt.AddWithOptions(&git.AddOptions{All: true}); err != nil {
		t.Fatal(err)
	}
	h, err := Commit(repo, wt, CommitMessage{Subject: "init"})
	if err != nil {
		t.Fatal(err)
	}

	_, privEd, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	sshPriv, err := ssh.NewSignerFromKey(privEd)
	if err != nil {
		t.Fatal(err)
	}

	id := Identity{Name: "Tagger", Email: "tag@example.com"}
	ref, err := SignedTag(repo, h, "v0.0.1", "signed release", id, func(payload []byte) ([]byte, error) {
		sig, err := sshsig.Sign(strings.NewReader(string(payload)), sshPriv, sign.HashAlgorithm, sign.Namespace)
		if err != nil {
			return nil, err
		}
		return sshsig.Armor(sig), nil
	})
	if err != nil {
		t.Fatalf("SignedTag: %v", err)
	}

	obj, err := repo.TagObject(ref.Hash())
	if err != nil {
		t.Fatalf("TagObject: %v", err)
	}
	if obj.PGPSignature == "" {
		t.Fatal("expected PGPSignature set on signed tag")
	}
	if !strings.Contains(obj.PGPSignature, "BEGIN SSH SIGNATURE") {
		t.Errorf("signature not armored: %q", obj.PGPSignature)
	}

	// Rebuild the signed payload and verify.
	payload, err := tagPayload("v0.0.1", h, "signed release", id)
	if err != nil {
		t.Fatalf("tagPayload: %v", err)
	}
	sig, err := sshsig.Unarmor([]byte(obj.PGPSignature))
	if err != nil {
		t.Fatalf("Unarmor: %v", err)
	}
	if err := sshsig.Verify(
		strings.NewReader(string(payload)), sig,
		sshPriv.PublicKey(), sign.HashAlgorithm, sign.Namespace,
	); err != nil {
		t.Fatalf("verify tag signature: %v", err)
	}
}
