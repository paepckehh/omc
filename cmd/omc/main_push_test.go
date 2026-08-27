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
	h, err := gitops.Commit(repo, wt, gitops.CommitMessage{Subject: "update", Body: "seed"})
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
	pushNothing(ui, repo, config.Config{PushKeyPath: keyPath})

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
	pushNothing(ui, repo, config.Config{PushKeyPath: filepath.Join(dir, "key")})
	if !strings.Contains(errBuf.String(), "push failed") {
		t.Errorf("expected a degradation warning, got: %q", errBuf.String())
	}
	if strings.Contains(out.String(), "pushed") {
		t.Errorf("a failed push must not report a push result: %q", out.String())
	}
}
