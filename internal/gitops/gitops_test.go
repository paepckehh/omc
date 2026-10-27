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
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/hiddeco/sshsig"
	"golang.org/x/crypto/ssh"

	"paepcke.de/omc/internal/sign"
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
	root, err := Commit(repo, CommitMessage{Subject: "initial"})
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
	second, err := Commit(repo, CommitMessage{Subject: "second", Body: "details"})
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
	h, err := Commit(repo, CommitMessage{Subject: "first"})
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
	if _, err := Commit(repo, CommitMessage{Subject: "initial"}); err != nil {
		t.Fatal(err)
	}

	commitFile(t, dir, "hello.txt", "hello world\n")
	if err := wt.AddWithOptions(&git.AddOptions{All: true}); err != nil {
		t.Fatal(err)
	}
	diff, files, err := StagedDiff(repo)
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

// TestStagedDiffSuppressesLockfile pins the lockfile suppression contract:
// flake.lock content (machine-generated pin hashes, pure noise for an LLM
// and worse: a huge context burn) never reaches the patch, the file is still
// listed in fileNames and represented by a one-line stub in the diff, other
// files keep their full unified diff, and the basename match covers
// subdirectories.
func TestStagedDiffSuppressesLockfile(t *testing.T) {
	repo, wt, dir := initTestRepo(t)
	lock := "{\n  \"nodes\": {\"nixpkgs\": \"sha256-deadbeef\"}\n}\n"
	commitFile(t, dir, "flake.lock", lock)
	commitFile(t, dir, "main.go", "package main\n")
	if err := os.MkdirAll(filepath.Join(dir, "vendor"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := wt.AddWithOptions(&git.AddOptions{All: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := Commit(repo, CommitMessage{Subject: "initial"}); err != nil {
		t.Fatal(err)
	}

	// Both a modified root flake.lock and a new nested one.
	commitFile(t, dir, "flake.lock", strings.ReplaceAll(lock, "deadbeef", "cafebabe"))
	commitFile(t, dir, "main.go", "package main\n\nfunc main() {}\n")
	commitFile(t, dir, filepath.Join("vendor", "flake.lock"), "{ \"pins\": {} }\n")
	if err := wt.AddWithOptions(&git.AddOptions{All: true}); err != nil {
		t.Fatal(err)
	}

	diff, files, err := StagedDiff(repo)
	if err != nil {
		t.Fatal(err)
	}

	// The lockfiles' content must not leak into the LLM prompt.
	for _, leak := range []string{"deadbeef", "cafebabe", `"pins"`} {
		if strings.Contains(diff, leak) {
			t.Errorf("diff leaked suppressed lockfile content %q:\n%s", leak, diff)
		}
	}
	// Each suppressed file is represented by exactly one stub line.
	for _, name := range []string{"flake.lock", "vendor/flake.lock"} {
		if got := strings.Count(diff, name); got < 1 {
			t.Errorf("diff missing suppressed file %q:\n%s", name, diff)
		} else if !strings.Contains(diff, name+" updated") {
			t.Errorf("diff missing stub body for %q:\n%s", name, diff)
		}
	}
	// Non-suppressed files keep their full unified diff.
	if !strings.Contains(diff, "+func main() {}") {
		t.Errorf("diff missing real content for main.go:\n%s", diff)
	}
	// fileNames still lists every changed file, suppressed ones included.
	found := map[string]bool{}
	for _, f := range files {
		found[f] = true
	}
	for _, name := range []string{"flake.lock", "vendor/flake.lock", "main.go"} {
		if !found[name] {
			t.Errorf("fileNames missing %q: %v", name, files)
		}
	}
}

// TestStagedDiffRootCommitSuppressesLockfile exercises the same suppression
// on the root commit, where the diff covers the full tree and there is no
// HEAD to diff against.
func TestStagedDiffRootCommitSuppressesLockfile(t *testing.T) {
	repo, wt, dir := initTestRepo(t)
	commitFile(t, dir, "flake.lock", "{\n  \"lock\": {\n    \"v\": 1\n  }\n}\n")
	commitFile(t, dir, "hello.txt", "hello\n")
	if err := wt.AddWithOptions(&git.AddOptions{All: true}); err != nil {
		t.Fatal(err)
	}
	diff, files, err := StagedDiff(repo)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(diff, `"lock"`) {
		t.Errorf("root-commit diff leaked flake.lock content:\n%s", diff)
	}
	if !strings.Contains(diff, "flake.lock updated") {
		t.Errorf("root-commit diff missing flake.lock stub:\n%s", diff)
	}
	if !strings.Contains(diff, "+hello") {
		t.Errorf("root-commit diff missing hello.txt content:\n%s", diff)
	}
	if len(files) != 2 {
		t.Errorf("files = %v, want both entries", files)
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
		t.Setenv("OMC_NAME", "Env User")
		t.Setenv("OMC_EMAIL", "env@example.com")
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
		t.Setenv("OMC_NAME", "")
		t.Setenv("OMC_EMAIL", "")
		t.Setenv("GIT_AUTHOR_NAME", "Git Author")
		t.Setenv("GIT_AUTHOR_EMAIL", "author@example.com")
		id := ResolveIdentity(repo)
		if id.Name != "Git Author" || id.Email != "author@example.com" {
			t.Errorf("got %+v, want git author identity", id)
		}
	})

	t.Run("repo config fallback", func(t *testing.T) {
		t.Setenv("OMC_NAME", "")
		t.Setenv("OMC_EMAIL", "")
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
		t.Setenv("OMC_NAME", "")
		t.Setenv("OMC_EMAIL", "")
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

	h, err := SignedCommit(repo, CommitMessage{Subject: "signed"}, func(payload []byte) ([]byte, error) {
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

func TestValidSemverTag(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"v0.0.0", true},
		{"v1.2.3", true},
		{"v999.888.777", true}, // large jumps in all segments
		{"0.1.2", true},        // bare, no leading v
		{"v01.2.3", false},     // leading zero
		{"v1.02.3", false},
		{"v1.2.03", false},
		{"v1.2.3-rc.1", false}, // pre-release suffix rejected
		{"v1.2.3+build", false},
		{"v1.2", false},     // missing segment
		{"v1.2.3.4", false}, // extra segment
		{"1.2.3.4", false},
		{"vA.2.3", false},
		{"", false},
		{"v-1.2.3", false},
	}
	for _, c := range cases {
		got := ValidSemverTag(c.in)
		if got != c.want {
			t.Errorf("ValidSemverTag(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestNormalizeTag(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"v1.2.3", "v1.2.3"},
		{"1.2.3", "v1.2.3"}, // bare gets the v prefix
		{"0.0.0", "v0.0.0"},
		{"v0.0.0", "v0.0.0"},
		{"v1.2.3-rc.1", "v1.2.3-rc.1"}, // invalid semver returned unchanged
		{"not-a-tag", "not-a-tag"},
		{"", ""},
	}
	for _, c := range cases {
		got := NormalizeTag(c.in)
		if got != c.want {
			t.Errorf("NormalizeTag(%q) = %q, want %q", c.in, got, c.want)
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
	h, err := Commit(repo, CommitMessage{Subject: "init"})
	if err != nil {
		t.Fatal(err)
	}
	id := Identity{Name: "Tagger", Email: "tag@example.com"}
	for _, name := range []string{"v0.1.0", "v0.0.5", "v1.0.0", "v0.10.0", "notsemver", "v0.1.0-rc.1", "v9.9.9junk", "v2.0.0.1"} {
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
	h, err := Commit(repo, CommitMessage{Subject: "init"})
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
	h, err := Commit(repo, CommitMessage{Subject: "init"})
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
	h, err := Commit(repo, CommitMessage{Subject: "init"})
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

	// Rebuild the signed payload from the STOREED tag object's fields so the
	// verification uses the exact tagger timestamp that was committed, not a
	// freshly generated time.Now().
	var payload bytes.Buffer
	fmt.Fprintf(&payload, "object %s\n", obj.Target)
	fmt.Fprintf(&payload, "type %s\n", obj.TargetType.Bytes())
	fmt.Fprintf(&payload, "tag %s\n", obj.Name)
	payload.WriteString("tagger ")
	if err := obj.Tagger.Encode(&payload); err != nil {
		t.Fatalf("encode tagger: %v", err)
	}
	payload.WriteString("\n\n")
	msg := strings.TrimSpace(obj.Message)
	if msg == "" {
		msg = "update"
	}
	payload.WriteString(msg)
	payload.WriteString("\n")

	sig, err := sshsig.Unarmor([]byte(obj.PGPSignature))
	if err != nil {
		t.Fatalf("Unarmor: %v", err)
	}
	if err := sshsig.Verify(
		strings.NewReader(payload.String()), sig,
		sshPriv.PublicKey(), sign.HashAlgorithm, sign.Namespace,
	); err != nil {
		t.Fatalf("verify tag signature: %v", err)
	}
}

// TestCommitMessageString covers the canonical commit message rendering:
// subject and body are joined with a blank line, and either part alone
// collapses to itself.
func TestCommitMessageString(t *testing.T) {
	cases := []struct {
		name string
		msg  CommitMessage
		want string
	}{
		{"empty", CommitMessage{}, ""},
		{"subject only", CommitMessage{Subject: "fix bug"}, "fix bug"},
		{"body only", CommitMessage{Body: "details"}, "details"},
		{"both", CommitMessage{Subject: "fix bug", Body: "details"}, "fix bug\n\ndetails"},
		{"trims whitespace", CommitMessage{Subject: "  s  ", Body: " b "}, "s\n\nb"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.msg.String(); got != tc.want {
				t.Errorf("String() = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestCommitSubjectFallback verifies the "update" fallback for empty
// messages.
func TestCommitSubjectFallback(t *testing.T) {
	if got := (CommitMessage{}).commitSubject(); got != "update" {
		t.Errorf("commitSubject() = %q, want update", got)
	}
	if got := (CommitMessage{Subject: "real"}).commitSubject(); got != "real" {
		t.Errorf("commitSubject() = %q, want real", got)
	}
}

// TestScout verifies the repository context diagnostic: worktree root, the
// configured remotes with URLs, and the highest semver tag.
func TestScout(t *testing.T) {
	repo, wt, dir := initTestRepo(t)
	commitFile(t, dir, "a.txt", "a")
	if err := wt.AddWithOptions(&git.AddOptions{All: true}); err != nil {
		t.Fatal(err)
	}
	h, err := Commit(repo, CommitMessage{Subject: "seed"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := CreateTag(repo, h, "v0.1.0", "seed",
		Identity{Name: "T", Email: "t@e"}); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.CreateRemote(&config.RemoteConfig{
		Name: "origin",
		URLs: []string{"https://example.com/repo.git"},
	}); err != nil {
		t.Fatal(err)
	}

	rc := Scout(repo, wt)
	if rc.Dir != dir {
		t.Errorf("Dir = %q, want %q", rc.Dir, dir)
	}
	if rc.LatestTag != "v0.1.0" {
		t.Errorf("LatestTag = %q, want v0.1.0", rc.LatestTag)
	}
	urls, ok := rc.Remotes["origin"]
	if !ok || len(urls) != 1 || urls[0] != "https://example.com/repo.git" {
		t.Errorf("Remotes[origin] = %v, want the configured URL", rc.Remotes)
	}
}

// TestScoutEmptyRepo verifies Scout tolerates a fresh repo without tags or
// remotes without failing.
func TestScoutEmptyRepo(t *testing.T) {
	repo, wt, _ := initTestRepo(t)
	rc := Scout(repo, wt)
	if rc.LatestTag != "" {
		t.Errorf("LatestTag = %q, want empty", rc.LatestTag)
	}
	if len(rc.Remotes) != 0 {
		t.Errorf("Remotes = %v, want empty", rc.Remotes)
	}
}

// TestSSHHostWithPort verifies the known_hosts lookup key: the default SSH
// port is filled in when the URL carries none.
func TestSSHHostWithPort(t *testing.T) {
	cases := []struct {
		url  string
		want string
	}{
		{"ssh://git@github.com:2222/org/repo.git", "github.com:2222"},
		{"ssh://git@github.com/org/repo.git", "github.com:22"},
	}
	for _, tc := range cases {
		t.Run(tc.url, func(t *testing.T) {
			ep, err := transport.NewEndpoint(tc.url)
			if err != nil {
				t.Fatal(err)
			}
			if got := sshHostWithPort(ep); got != tc.want {
				t.Errorf("sshHostWithPort = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestGetSSHAuthEmptyURLs guards the empty remote-URL panic: no URLs means
// no explicit auth, not a crash.
func TestGetSSHAuthEmptyURLs(t *testing.T) {
	auth, err := getSSHAuth("", nil, sign.TouchConfig{})
	if err != nil {
		t.Fatalf("getSSHAuth with no URLs: %v", err)
	}
	if auth != nil {
		t.Errorf("auth = %v, want nil", auth)
	}
}

// TestPushToRemoteURLessRemote verifies a remote configured without URLs
// degrades to a clear error instead of panicking. CreateRemote and SetConfig
// both reject empty URLs, so the URL-less remote is injected by writing the
// raw .git/config, which is how a hand-edited repo reaches this state.
func TestPushToRemoteURLessRemote(t *testing.T) {
	_, _, dir := initTestRepo(t)
	cfgPath := filepath.Join(dir, ".git", "config")
	if err := os.WriteFile(cfgPath, []byte("[core]\n\trepositoryformatversion = 0\n\n[remote \"origin\"]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Re-open so the on-disk config is read fresh.
	repo, err := git.PlainOpen(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := PushToRemote(repo, "", sign.TouchConfig{}); err == nil {
		t.Fatal("expected error for remote without URLs")
	}
}

// TestPushToRemoteDeterministicFallback verifies the fallback remote pick
// is deterministic: with no "origin", the lexicographically first remote
// with URLs is used regardless of map iteration order.
func TestPushToRemoteDeterministicFallback(t *testing.T) {
	bare := newRemoteBare(t, "omcf")
	_ = bare

	repo, wt, dir := initTestRepo(t)
	for _, name := range []string{"zeta", "alpha"} {
		if _, err := repo.CreateRemote(&config.RemoteConfig{
			Name: name,
			URLs: []string{"omcf://omc/repos/remote.git"},
		}); err != nil {
			t.Fatal(err)
		}
	}
	commitFile(t, dir, "a.txt", "a")
	if err := wt.AddWithOptions(&git.AddOptions{All: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := Commit(repo, CommitMessage{Subject: "seed"}); err != nil {
		t.Fatal(err)
	}

	res, err := PushToRemote(repo, "", sign.TouchConfig{})
	if err != nil {
		t.Fatalf("PushToRemote: %v", err)
	}
	if res.Remote != "alpha" {
		t.Errorf("Remote = %q, want alpha (deterministic pick)", res.Remote)
	}
}
