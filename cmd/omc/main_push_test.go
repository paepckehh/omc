package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-git/go-git/v5"
	gitconfig "github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/transport/client"
	"github.com/go-git/go-git/v5/plumbing/transport/server"

	"paepcke.de/omc/internal/config"
	"paepcke.de/omc/internal/gitops"
	"paepcke.de/omc/internal/output"
	"paepcke.de/omc/internal/sign"
)

// TestPushNothingPushesTags verifies the bug fix: when there is nothing to
// commit or tag, but a remote is configured and the local tags are ahead
// (a previous run committed and tagged locally without pushing), the
// push step still runs and publishes the pending tags.
func TestPushNothingPushesTags(t *testing.T) {
	dir := t.TempDir()
	repo, err := git.PlainInit(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_AUTHOR_NAME", "Test User")
	t.Setenv("GIT_AUTHOR_EMAIL", "test@example.com")
	t.Setenv("GIT_COMMITTER_NAME", "Test User")
	t.Setenv("GIT_COMMITTER_EMAIL", "test@example.com")
	wt, err := repo.Worktree()
	if err != nil {
		t.Fatal(err)
	}

	// A prior "omc" run committed and tagged v0.0.1, but the push was
	// skipped or failed. Recreate that state with the real helpers.
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := wt.AddWithOptions(&git.AddOptions{All: true}); err != nil {
		t.Fatal(err)
	}
	h, err := gitops.Commit(repo, gitops.CommitMessage{Subject: "update", Body: "seed"})
	if err != nil {
		t.Fatal(err)
	}
	tag, err := gitops.CreateTag(repo, h, "v0.0.1", "update",
		gitops.Identity{Name: "Test User", Email: "test@example.com"})
	if err != nil {
		t.Fatal(err)
	}

	// Wire up the fake remote (in-process protocol + bare repo).
	remoteDir := t.TempDir()
	bare, err := git.PlainInit(remoteDir, true)
	if err != nil {
		t.Fatal(err)
	}
	client.InstallProtocol("omcpt", server.NewServer(server.MapLoader{
		"omcpt://omc/repos/remote.git": bare.Storer,
	}))
	if _, err := repo.CreateRemote(&gitconfig.RemoteConfig{
		Name: "origin",
		URLs: []string{"omcpt://omc/repos/remote.git"},
	}); err != nil {
		t.Fatal(err)
	}

	// The remote must not know about the branch or tag yet.
	if _, err := bare.Storer.Reference(plumbing.NewBranchReferenceName("master")); err == nil {
		t.Fatal("remote already has the branch")
	}
	if _, err := bare.Storer.Reference(tag.Name()); err == nil {
		t.Fatal("remote already has the tag")
	}

	// Run the helper under test with a non-TTY UI (structured logs) and a
	// configured push key. The fake protocol is not SSH, so the empty key
	// file is never actually read; it just gates the push like
	// OMC_PUSH_KEY_PATH does.
	keyPath := filepath.Join(dir, "push-key")
	if err := os.WriteFile(keyPath, []byte("unused"), 0o600); err != nil {
		t.Fatal(err)
	}
	var out, errBuf bytes.Buffer
	ui := output.New(&out, &errBuf)
	pushNothing(ui, repo, config.Config{PushKeyPath: keyPath}, sign.TouchConfig{})

	// The branch must have been pushed to the remote.
	gotBranch, err := bare.Storer.Reference(plumbing.NewBranchReferenceName("master"))
	if err != nil {
		t.Fatalf("remote branch not pushed: %v", err)
	}
	if gotBranch.Hash() != h {
		t.Errorf("remote master = %s, want %s", gotBranch.Hash(), h)
	}

	// The pending tag must have been pushed too.
	gotTag, err := bare.Storer.Reference(tag.Name())
	if err != nil {
		t.Fatalf("remote tag not pushed: %v", err)
	}
	if gotTag.Hash() != tag.Hash() {
		t.Errorf("remote tag = %s, want %s", gotTag.Hash(), tag.Hash())
	}
	if err := bare.Storer.HasEncodedObject(tag.Hash()); err != nil {
		t.Error("remote tag object missing")
	}

	// The structured push result must be reported on stdout.
	if !strings.Contains(out.String(), "pushed") {
		t.Errorf("output missing push notice: %s", out.String())
	}
}

// TestPushNothingWarnsOnTagCollision pins the fail-proof tag sync contract
// end to end through the pipeline helper: when the local tag state is stale
// (the upstream tag pull failed or never ran) and a local tag collides with
// a same-named remote tag pointing at a different object, the push must
// leave the remote tag untouched, still push the branch, and warn about the
// held-back tag instead of silently force-pushing over it.
func TestPushNothingWarnsOnTagCollision(t *testing.T) {
	seedDir := t.TempDir()
	seed, err := git.PlainInit(seedDir, false)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_AUTHOR_NAME", "Test User")
	t.Setenv("GIT_AUTHOR_EMAIL", "test@example.com")
	t.Setenv("GIT_COMMITTER_NAME", "Test User")
	t.Setenv("GIT_COMMITTER_EMAIL", "test@example.com")
	seedWt, err := seed.Worktree()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(seedDir, "a.txt"), []byte("a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := seedWt.AddWithOptions(&git.AddOptions{All: true}); err != nil {
		t.Fatal(err)
	}
	seedHash, err := gitops.Commit(seed, gitops.CommitMessage{Subject: "seed"})
	if err != nil {
		t.Fatal(err)
	}
	remoteTag, err := gitops.CreateTag(seed, seedHash, "v0.0.1", "seed",
		gitops.Identity{Name: "Test User", Email: "test@example.com"})
	if err != nil {
		t.Fatal(err)
	}

	remoteDir := t.TempDir()
	bare, err := git.PlainInit(remoteDir, true)
	if err != nil {
		t.Fatal(err)
	}
	client.InstallProtocol("omccl", server.NewServer(server.MapLoader{
		"omccl://omc/repos/remote.git": bare.Storer,
	}))
	if _, err := seed.CreateRemote(&gitconfig.RemoteConfig{
		Name: "origin",
		URLs: []string{"omccl://omc/repos/remote.git"},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := gitops.PushToRemote(seed, "", sign.TouchConfig{}); err != nil {
		t.Fatalf("seed push: %v", err)
	}

	// The local clone knows the branch but not the tag (stale tag state).
	cloneDir := t.TempDir()
	repo, err := git.PlainClone(cloneDir, false, &git.CloneOptions{
		URL: "omccl://omc/repos/remote.git",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.Storer.RemoveReference(plumbing.NewTagReferenceName("v0.0.1")); err != nil {
		t.Fatal(err)
	}
	wt, err := repo.Worktree()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cloneDir, "b.txt"), []byte("b\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := wt.AddWithOptions(&git.AddOptions{All: true}); err != nil {
		t.Fatal(err)
	}
	localHash, err := gitops.Commit(repo, gitops.CommitMessage{Subject: "local"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := gitops.CreateTag(repo, localHash, "v0.0.1", "stale bump",
		gitops.Identity{Name: "Test User", Email: "test@example.com"}); err != nil {
		t.Fatal(err)
	}

	var out, errBuf bytes.Buffer
	ui := output.New(&out, &errBuf)
	keyPath := filepath.Join(cloneDir, "push-key")
	if err := os.WriteFile(keyPath, []byte("unused"), 0o600); err != nil {
		t.Fatal(err)
	}
	pushNothing(ui, repo, config.Config{PushKeyPath: keyPath}, sign.TouchConfig{})

	if !strings.Contains(errBuf.String(), "v0.0.1 exists on the remote pointing elsewhere") {
		t.Errorf("expected a held-back tag warning, got: %q", errBuf.String())
	}
	// The remote tag must still be the seed machine's tag object.
	got, rerr := bare.Storer.Reference(plumbing.NewTagReferenceName("v0.0.1"))
	if rerr != nil {
		t.Fatalf("remote tag missing: %v", rerr)
	}
	if got.Hash() != remoteTag.Hash() {
		t.Errorf("remote tag overwritten: %s, want %s", got.Hash(), remoteTag.Hash())
	}
	// The branch was still published.
	if got, berr := bare.Storer.Reference(plumbing.NewBranchReferenceName("master")); berr != nil || got.Hash() != localHash {
		t.Errorf("remote master = %v err=%v, want %s", got, berr, localHash)
	}
}

// TestPushNothingNoRemote verifies the degrade path: a clean tree with no
// remote logs a warning and does not fail.
func TestPushNothingNoRemote(t *testing.T) {
	dir := t.TempDir()
	repo, err := git.PlainInit(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	var out, errBuf bytes.Buffer
	ui := output.New(&out, &errBuf)
	// The key path is irrelevant when there is no remote to push to, but
	// it must be non-empty for the push to be attempted at all.
	if err := os.WriteFile(filepath.Join(dir, "key"), []byte("unused"), 0o600); err != nil {
		t.Fatal(err)
	}
	pushNothing(ui, repo, config.Config{PushKeyPath: filepath.Join(dir, "key")}, sign.TouchConfig{})
	if !strings.Contains(errBuf.String(), "push failed") {
		t.Errorf("expected a degradation warning, got: %q", errBuf.String())
	}
	if strings.Contains(out.String(), "pushed") {
		t.Errorf("a failed push must not report a push result: %q", out.String())
	}
}
