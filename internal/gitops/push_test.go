package gitops

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/go-git/go-git/v5"

	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/storer"
	"github.com/go-git/go-git/v5/plumbing/transport/client"
	"github.com/go-git/go-git/v5/plumbing/transport/server"
	"paepcke.de/omc/internal/sign"
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
	h, err := Commit(repo, CommitMessage{Subject: "push me"})
	if err != nil {
		t.Fatal(err)
	}

	// Create the annotated tag that the pipeline would create.
	tag := createTestTag(t, repo, h)

	res, err := PushToRemote(repo, "", sign.TouchConfig{})
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
	if _, err := PushToRemote(repo, "", sign.TouchConfig{}); err == nil {
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
	if _, err := Commit(repo, CommitMessage{Subject: "only commit"}); err != nil {
		t.Fatal(err)
	}

	// First push succeeds; second push finds nothing new on the branch.
	if _, err := PushToRemote(repo, "", sign.TouchConfig{}); err != nil {
		t.Fatalf("first push: %v", err)
	}
	if _, err := PushToRemote(repo, "", sign.TouchConfig{}); err != nil {
		t.Fatalf("second push should be up-to-date, got: %v", err)
	}
	_ = bare
}

// TestPushToRemoteUpToDateBranchPushesPendingTags verifies the stranded-tag
// bug: when the remote branch is already up to date but a local tag never made
// it upstream (a previous run pushed the branch but its tag pass failed),
// PushToRemote must still run the tag pass. The old code returned early on
// NoErrAlreadyUpToDate from the branch pass, leaving the pending tag local
// forever — every clean-tree run repeated the early return.
func TestPushToRemoteUpToDateBranchPushesPendingTags(t *testing.T) {
	bare := newRemoteBare(t, "omcpt")

	repo, wt, dir := initTestRepo(t)
	if _, err := repo.CreateRemote(&config.RemoteConfig{
		Name: "origin",
		URLs: []string{"omcpt://omc/repos/remote.git"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := wt.AddWithOptions(&git.AddOptions{All: true}); err != nil {
		t.Fatal(err)
	}
	h, err := Commit(repo, CommitMessage{Subject: "seed"})
	if err != nil {
		t.Fatal(err)
	}
	tag, err := CreateTag(repo, h, "v0.0.1", "seed",
		Identity{Name: "Test User", Email: "test@example.com"})
	if err != nil {
		t.Fatal(err)
	}

	// Recreate the stranded state: the branch was pushed, the tag pass
	// failed (here: never ran). Push the branch refspec only.
	remote := git.NewRemote(repo.Storer, &config.RemoteConfig{
		Name: "origin",
		URLs: []string{"omcpt://omc/repos/remote.git"},
	})
	if err := remote.Push(&git.PushOptions{
		RemoteName: "origin",
		RefSpecs:   []config.RefSpec{config.RefSpec("refs/heads/master:refs/heads/master")},
	}); err != nil {
		t.Fatalf("seed branch push: %v", err)
	}
	if _, err := bare.Storer.Reference(plumbing.NewBranchReferenceName("master")); err != nil {
		t.Fatalf("remote branch missing after seed push: %v", err)
	}
	if _, err := bare.Storer.Reference(tag.Name()); err == nil {
		t.Fatal("remote already has the tag; test setup is wrong")
	}

	// The branch is now up to date. PushToRemote must not stop there.
	res, err := PushToRemote(repo, "", sign.TouchConfig{})
	if err != nil {
		t.Fatalf("PushToRemote: %v", err)
	}
	if !res.Tags {
		t.Error("Tags = false, want true (pending tag must be pushed)")
	}
	got, err := bare.Storer.Reference(tag.Name())
	if err != nil {
		t.Fatalf("remote tag still missing after push: %v", err)
	}
	if got.Hash() != tag.Hash() {
		t.Errorf("remote tag = %s, want %s", got.Hash(), tag.Hash())
	}
	if err := bare.Storer.HasEncodedObject(tag.Hash()); err != nil {
		t.Error("remote tag object missing")
	}
}

// TestPushToRemoteNeverOverwritesRemoteTag pins the silent-clobber bug on the
// pull-failure path: when the pre-tag upstream pull fails, the bump runs on
// stale local state and can generate a name that already exists upstream.
// ensureTagAbsent only consults LOCAL refs, so the colliding tag is created
// locally without error — the old forced tag refspec then overwrote the
// remote tag without a word. PushToRemote must instead hold the colliding
// tag back, leave the remote tag untouched, and report the error.
func TestPushToRemoteNeverOverwritesRemoteTag(t *testing.T) {
	bare := newRemoteBare(t, "omcnc")

	// Another machine: two commits and a tag v0.0.1, pushed upstream.
	seed, seedWt, seedDir := initTestRepo(t)
	if _, err := seed.CreateRemote(&config.RemoteConfig{
		Name: "origin",
		URLs: []string{"omcnc://omc/repos/remote.git"},
	}); err != nil {
		t.Fatal(err)
	}
	id := Identity{Name: "Test User", Email: "test@example.com"}
	if err := os.WriteFile(filepath.Join(seedDir, "seed.txt"), []byte("seed"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := seedWt.AddWithOptions(&git.AddOptions{All: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := Commit(seed, CommitMessage{Subject: "seed"}); err != nil {
		t.Fatal(err)
	}
	seedHash, err := Commit(seed, CommitMessage{Subject: "seed two"})
	if err != nil {
		t.Fatal(err)
	}
	remoteTag, err := CreateTag(seed, seedHash, "v0.0.1", "seed", id)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := PushToRemote(seed, "", sign.TouchConfig{}); err != nil {
		t.Fatalf("seed push: %v", err)
	}

	// This machine cloned before the tag existed (or its upstream tag pull
	// failed): simulate the stale state by dropping the cloned tag ref.
	cloneDir := t.TempDir()
	repo, err := git.PlainClone(cloneDir, false, &git.CloneOptions{
		URL: "omcnc://omc/repos/remote.git",
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
	if err := os.WriteFile(filepath.Join(cloneDir, "local.txt"), []byte("local"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := wt.AddWithOptions(&git.AddOptions{All: true}); err != nil {
		t.Fatal(err)
	}
	localHash, err := Commit(repo, CommitMessage{Subject: "local"})
	if err != nil {
		t.Fatal(err)
	}
	// The stale bump from zero lands on the same name as the remote tag.
	localTag, err := CreateTag(repo, localHash, "v0.0.1", "local", id)
	if err != nil {
		t.Fatal(err)
	}

	res, err := PushToRemote(repo, "", sign.TouchConfig{})
	if err == nil {
		t.Fatal("PushToRemote must fail when the only tag collides with the remote")
	}
	if res.Tags {
		t.Error("Tags = true, want false (nothing was published)")
	}
	if len(res.SkippedTags) != 1 || res.SkippedTags[0] != "v0.0.1" {
		t.Errorf("SkippedTags = %v, want [v0.0.1]", res.SkippedTags)
	}

	// The remote tag must still point at the other machine's tag object.
	got, rerr := bare.Storer.Reference(plumbing.NewTagReferenceName("v0.0.1"))
	if rerr != nil {
		t.Fatalf("remote tag missing: %v", rerr)
	}
	if got.Hash() != remoteTag.Hash() {
		t.Errorf("remote tag overwritten: %s, want %s", got.Hash(), remoteTag.Hash())
	}
	// The branch was still pushed; the local tag still exists locally.
	assertRemoteRef(t, bare.Storer, plumbing.NewBranchReferenceName("master"), localHash)
	if got, lerr := repo.Storer.Reference(plumbing.NewTagReferenceName("v0.0.1")); lerr != nil || got.Hash() != localTag.Hash() {
		t.Errorf("local tag changed: %s err=%v, want %s", got.Hash(), lerr, localTag.Hash())
	}
}

// TestPushToRemoteCollisionStillPublishesOtherTags pins the per-tag fallback:
// a wildcard tag pass that hits one colliding tag aborts as a whole
// (go-git rejects non-forced non-fast-forward updates client-side), but the
// non-colliding pending tags must still publish in the same run instead of
// being stranded until the colliding name is resolved.
func TestPushToRemoteCollisionStillPublishesOtherTags(t *testing.T) {
	bare := newRemoteBare(t, "omcpc")

	seed, seedWt, seedDir := initTestRepo(t)
	if _, err := seed.CreateRemote(&config.RemoteConfig{
		Name: "origin",
		URLs: []string{"omcpc://omc/repos/remote.git"},
	}); err != nil {
		t.Fatal(err)
	}
	id := Identity{Name: "Test User", Email: "test@example.com"}
	if err := os.WriteFile(filepath.Join(seedDir, "seed.txt"), []byte("seed"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := seedWt.AddWithOptions(&git.AddOptions{All: true}); err != nil {
		t.Fatal(err)
	}
	seedHash, err := Commit(seed, CommitMessage{Subject: "seed"})
	if err != nil {
		t.Fatal(err)
	}
	remoteTag, err := CreateTag(seed, seedHash, "v0.0.1", "seed", id)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := PushToRemote(seed, "", sign.TouchConfig{}); err != nil {
		t.Fatalf("seed push: %v", err)
	}

	// Local: a clone that does not know the upstream v0.0.1 (stale tag
	// state) plus a fresh, free v0.0.5 on a new commit.
	cloneDir := t.TempDir()
	repo, err := git.PlainClone(cloneDir, false, &git.CloneOptions{
		URL: "omcpc://omc/repos/remote.git",
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
	if err := os.WriteFile(filepath.Join(cloneDir, "local.txt"), []byte("local"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := wt.AddWithOptions(&git.AddOptions{All: true}); err != nil {
		t.Fatal(err)
	}
	localHash, err := Commit(repo, CommitMessage{Subject: "local"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := CreateTag(repo, localHash, "v0.0.1", "stale bump", id); err != nil {
		t.Fatal(err)
	}
	freeTag, err := CreateTag(repo, localHash, "v0.0.5", "free", id)
	if err != nil {
		t.Fatal(err)
	}

	res, err := PushToRemote(repo, "", sign.TouchConfig{})
	if err != nil {
		t.Fatalf("PushToRemote must degrade, not fail: %v", err)
	}
	if !res.Tags {
		t.Error("Tags = false, want true (the free tag was published)")
	}
	if len(res.SkippedTags) != 1 || res.SkippedTags[0] != "v0.0.1" {
		t.Errorf("SkippedTags = %v, want [v0.0.1]", res.SkippedTags)
	}

	// Remote: the colliding tag untouched, the free tag published.
	got, rerr := bare.Storer.Reference(plumbing.NewTagReferenceName("v0.0.1"))
	if rerr != nil {
		t.Fatalf("remote v0.0.1 missing: %v", rerr)
	}
	if got.Hash() != remoteTag.Hash() {
		t.Errorf("remote v0.0.1 overwritten: %s, want %s", got.Hash(), remoteTag.Hash())
	}
	assertRemoteRef(t, bare.Storer, plumbing.NewTagReferenceName("v0.0.5"), freeTag.Hash())
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

// TestPushToRemoteBackslashEntry is a regression guard for a go-git
// enumeration-time path check. go-git v5.19.1 added pathutil.ValidTreePath to
// TreeWalker.Next; since Remote.Push walks every reachable tree via
// revlist.Objects to build the packfile, a single tree entry whose name that
// check rejects (here: a lone backslash, which git itself stores fine) aborts
// the whole push with "invalid path". omc pins go-git to v5.19.0 (the last
// release before that check) so the enumeration walks object names verbatim.
// This test builds such a repo by writing raw objects (the worktree Add path
// also rejects the name, so the tree is assembled directly in the store) and
// asserts the push still succeeds. If go-git is ever upgraded past v5.19.0
// and the regression returns, this test fails loudly.
func TestPushToRemoteBackslashEntry(t *testing.T) {
	bare := newRemoteBare(t, "omcbs")

	repo, _, _ := initTestRepo(t)
	if _, err := repo.CreateRemote(&config.RemoteConfig{
		Name: "origin",
		URLs: []string{"omcbs://omc/repos/remote.git"},
	}); err != nil {
		t.Fatal(err)
	}

	commit := writeBackslashCommit(t, repo)
	if err := repo.Storer.SetReference(plumbing.NewHashReference(
		plumbing.NewBranchReferenceName("master"), commit,
	)); err != nil {
		t.Fatal(err)
	}
	if err := repo.Storer.SetReference(plumbing.NewSymbolicReference(
		plumbing.HEAD, plumbing.NewBranchReferenceName("master"),
	)); err != nil {
		t.Fatal(err)
	}

	if _, err := PushToRemote(repo, "", sign.TouchConfig{}); err != nil {
		t.Fatalf("PushToRemote with backslash-named tree entry failed: %v\n"+
			"go-git's revlist must enumerate object names verbatim; this fails "+
			"when go-git is upgraded past v5.19.0 (see go.mod pin comment).", err)
	}
	assertRemoteRef(t, bare.Storer, plumbing.NewBranchReferenceName("master"), commit)
}

// writeBackslashCommit stores a blob plus a tree that names the blob "\" (a
// single backslash), then a commit pointing at that tree. The objects are
// written straight into the storer to bypass the worktree path validator,
// which would otherwise refuse the name before it ever reaches the store.
// The returned hash is the commit's hash.
func writeBackslashCommit(t *testing.T, repo *git.Repository) plumbing.Hash {
	t.Helper()

	blob := &plumbing.MemoryObject{}
	blob.SetType(plumbing.BlobObject)
	bw, err := blob.Writer()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := bw.Write([]byte("x")); err != nil {
		t.Fatal(err)
	}
	blobHash, err := repo.Storer.SetEncodedObject(blob)
	if err != nil {
		t.Fatal(err)
	}

	treeObj := &plumbing.MemoryObject{}
	tree := &object.Tree{Entries: []object.TreeEntry{{
		Name: "\\",
		Mode: 0o100644,
		Hash: blobHash,
	}}}
	if err := tree.Encode(treeObj); err != nil {
		t.Fatal(err)
	}
	treeHash, err := repo.Storer.SetEncodedObject(treeObj)
	if err != nil {
		t.Fatal(err)
	}

	commitObj := &plumbing.MemoryObject{}
	sig := object.Signature{Name: "T", Email: "t@t"}
	commit := &object.Commit{
		Author:    sig,
		Committer: sig,
		Message:   "backslash entry",
		TreeHash:  treeHash,
	}
	if err := commit.Encode(commitObj); err != nil {
		t.Fatal(err)
	}
	commitHash, err := repo.Storer.SetEncodedObject(commitObj)
	if err != nil {
		t.Fatal(err)
	}
	return commitHash
}

// TestPushTagsMultipleCollisionsStillPublishesFreeTag pins the no-loop
// contract of the tag pass: the wildcard push aborts on the first colliding
// tag (go-git's nameless "object not found"), so one authenticated listing
// computes the whole colliding set and a single retry publishes every free
// tag. No per-tag loop: with an FIDO2 push key every extra session is
// another smartcard touch, so the pass is bounded at one listing plus one
// retry no matter how many tags collide.
func TestPushTagsNoRetryLoopAfterSecondCollision(t *testing.T) {
	bare := newRemoteBare(t, "omcnl")

	seed, seedWt, seedDir := initTestRepo(t)
	if _, err := seed.CreateRemote(&config.RemoteConfig{
		Name: "origin",
		URLs: []string{"omcnl://omc/repos/remote.git"},
	}); err != nil {
		t.Fatal(err)
	}
	id := Identity{Name: "Test User", Email: "test@example.com"}
	if err := os.WriteFile(filepath.Join(seedDir, "seed.txt"), []byte("seed"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := seedWt.AddWithOptions(&git.AddOptions{All: true}); err != nil {
		t.Fatal(err)
	}
	seedHash, err := Commit(seed, CommitMessage{Subject: "seed"})
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"v0.0.1", "v0.0.2"} {
		if _, err := CreateTag(seed, seedHash, name, "seed", id); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := PushToRemote(seed, "", sign.TouchConfig{}); err != nil {
		t.Fatalf("seed push: %v", err)
	}

	// Local: a clone blind to both upstream tags (stale state) that bumps
	// into both names again, plus one free tag that would publish.
	cloneDir := t.TempDir()
	repo, err := git.PlainClone(cloneDir, false, &git.CloneOptions{
		URL: "omcnl://omc/repos/remote.git",
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"v0.0.1", "v0.0.2"} {
		if err := repo.Storer.RemoveReference(plumbing.NewTagReferenceName(name)); err != nil {
			t.Fatal(err)
		}
	}
	wt, err := repo.Worktree()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cloneDir, "local.txt"), []byte("local"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := wt.AddWithOptions(&git.AddOptions{All: true}); err != nil {
		t.Fatal(err)
	}
	localHash, err := Commit(repo, CommitMessage{Subject: "local"})
	if err != nil {
		t.Fatal(err)
	}
	remoteTag1, err := bare.Storer.Reference(plumbing.NewTagReferenceName("v0.0.1"))
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"v0.0.1", "v0.0.2", "v0.0.5"} {
		if _, err := CreateTag(repo, localHash, name, "stale bump", id); err != nil {
			t.Fatal(err)
		}
	}

	res, err := PushToRemote(repo, "", sign.TouchConfig{})
	if err != nil {
		t.Fatalf("PushToRemote must degrade, not fail: %v", err)
	}
	if len(res.SkippedTags) != 2 || res.SkippedTags[0] != "v0.0.1" || res.SkippedTags[1] != "v0.0.2" {
		t.Errorf("SkippedTags = %v, want [v0.0.1 v0.0.2] (the full colliding set)", res.SkippedTags)
	}
	if !res.Tags {
		t.Error("Tags = false, want true (the free tag was published)")
	}

	// The remote tags are untouched and the free tag published in the
	// single retry — no per-tag session loop even with two collisions.
	got, rerr := bare.Storer.Reference(plumbing.NewTagReferenceName("v0.0.1"))
	if rerr != nil || got.Hash() != remoteTag1.Hash() {
		t.Errorf("remote v0.0.1 must stay the seed tag: %s err=%v", got.Hash(), rerr)
	}
	remoteTag2, err := bare.Storer.Reference(plumbing.NewTagReferenceName("v0.0.2"))
	if err != nil {
		t.Fatal(err)
	}
	if got, rerr := bare.Storer.Reference(plumbing.NewTagReferenceName("v0.0.2")); rerr != nil || got.Hash() != remoteTag2.Hash() {
		t.Errorf("remote v0.0.2 must stay the seed tag: %s err=%v", got.Hash(), rerr)
	}
	localTag5, err := repo.Storer.Reference(plumbing.NewTagReferenceName("v0.0.5"))
	if err != nil {
		t.Fatal(err)
	}
	assertRemoteRef(t, bare.Storer, plumbing.NewTagReferenceName("v0.0.5"), localTag5.Hash())
	assertRemoteRef(t, bare.Storer, plumbing.NewBranchReferenceName("master"), localHash)
}
