package gitops

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/storer"
	"github.com/go-git/go-git/v5/plumbing/transport/client"
	"github.com/go-git/go-git/v5/plumbing/transport/server"
)

// TestPushToRemote pushes a local repo's branch and tags to a remote bare
// repo entirely in-process (no git binary, no network), then verifies the
// remote storage actually received the refs.
func TestPushToRemote(t *testing.T) {
	bare := newRemoteBare(t, "omco")

	repo, wt, dir := initTestRepo(t)
	// PlainInit creates a default "master" HEAD; switch to an explicit
	// branch so the remote ref name is deterministic. The default branch
	// ref is removed and HEAD is re-pointed at the feature branch. Note:
	// the feature ref is NOT pre-created at ZeroHash — that would make
	// Commit record a ZeroHash parent and break the push.
	if err := repo.Storer.RemoveReference(plumbing.NewBranchReferenceName("master")); err != nil {
		t.Fatal(err)
	}
	if err := repo.Storer.SetReference(plumbing.NewSymbolicReference(
		plumbing.HEAD, plumbing.NewBranchReferenceName("feature"),
	)); err != nil {
		t.Fatal(err)
	}
	// PlainInit does not wire a remote; add one pointing at the fake
	// protocol endpoint.
	if _, err := repo.CreateRemote(&config.RemoteConfig{
		Name: "origin",
		URLs: []string{"omco://omc/repos/remote.git"},
	}); err != nil {
		t.Fatal(err)
	}

	// Create one commit on the branch.
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := wt.AddWithOptions(&git.AddOptions{All: true}); err != nil {
		t.Fatal(err)
	}
	h, err := Commit(repo, wt, CommitMessage{Subject: "push me"})
	if err != nil {
		t.Fatal(err)
	}

	// Create the annotated tag that the pipeline would create.
	tag := createTestTag(t, repo, h)

	res, err := PushToRemote(repo, "")
	if err != nil {
		t.Fatalf("PushToRemote: %v", err)
	}
	if res.Remote != "origin" {
		t.Errorf("Remote = %q, want origin", res.Remote)
	}
	if res.Branch != "feature" {
		t.Errorf("Branch = %q, want feature", res.Branch)
	}
	if !res.Tags {
		t.Error("Tags = false, want true")
	}

	// Verify the remote storage now holds both refs.
	assertRemoteRef(t, bare.Storer, plumbing.NewBranchReferenceName("feature"), h)
	gotTag, err := bare.Storer.Reference(tag.Name())
	if err != nil {
		t.Fatalf("remote tag ref: %v", err)
	}
	if gotTag.Hash() != tag.Hash() {
		t.Errorf("remote tag = %s, want %s", gotTag.Hash(), tag.Hash())
	}
	// The tag object itself must be present, not just the ref.
	if err := bare.Storer.HasEncodedObject(tag.Hash()); err != nil {
		t.Errorf("remote tag object missing: %v", err)
	}
}

// TestPushToRemoteNoRemote verifies the degrade path: no remote means a
// clear error but nothing else happens.
func TestPushToRemoteNoRemote(t *testing.T) {
	repo, _, _ := initTestRepo(t)
	if _, err := PushToRemote(repo, ""); err == nil {
		t.Fatal("expected error for repo without remote")
	}
}

// TestPushToRemoteUpToDate verifies that a repo that already matches the
// remote is reported as success (NoErrAlreadyUpToDate treated as done).
func TestPushToRemoteUpToDate(t *testing.T) {
	bare := newRemoteBare(t, "omcu")

	repo, wt, dir := initTestRepo(t)
	if _, err := repo.CreateRemote(&config.RemoteConfig{
		Name: "origin",
		URLs: []string{"omcu://omc/repos/remote.git"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := wt.AddWithOptions(&git.AddOptions{All: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := Commit(repo, wt, CommitMessage{Subject: "only commit"}); err != nil {
		t.Fatal(err)
	}

	// First push succeeds; second push finds nothing new on the branch.
	if _, err := PushToRemote(repo, ""); err != nil {
		t.Fatalf("first push: %v", err)
	}
	if _, err := PushToRemote(repo, ""); err != nil {
		t.Fatalf("second push should be up-to-date, got: %v", err)
	}
	_ = bare
}

// newRemoteBare creates a bare repo and routes the given protocol to an
// in-process server that serves that repo's storage, so pushes never touch
// the network or a git binary.
func newRemoteBare(t *testing.T, protocol string) *git.Repository {
	t.Helper()
	remoteDir := t.TempDir()
	bare, err := git.PlainInit(remoteDir, true)
	if err != nil {
		t.Fatal(err)
	}
	client.InstallProtocol(protocol, server.NewServer(server.MapLoader{
		protocol + "://omc/repos/remote.git": bare.Storer,
	}))
	return bare
}

// createTestTag creates an annotated tag like the pipeline does.
func createTestTag(t *testing.T, repo *git.Repository, h plumbing.Hash) *plumbing.Reference {
	t.Helper()
	ref, err := CreateTag(repo, h, "v0.0.1", "push me",
		Identity{Name: "Test User", Email: "test@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	return ref
}

// assertRemoteRef checks that remote's storer holds ref at hash.
func assertRemoteRef(t *testing.T, s storer.ReferenceStorer, name plumbing.ReferenceName, want plumbing.Hash) {
	t.Helper()
	got, err := s.Reference(name)
	if err != nil {
		t.Fatalf("remote ref %s: %v", name, err)
	}
	if got.Hash() != want {
		t.Errorf("remote %s = %s, want %s", name, got.Hash(), want)
	}
}
