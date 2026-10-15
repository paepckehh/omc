package gitops

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/go-git/go-git/v5"

	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"paepcke.de/omc/internal/sign"
)

// TestPullTagsAvoidsUpstreamCollision reproduces the tag collision bug and its
// fix: a repository that does not know the remote's tags (a fresh clone by
// copy, or a pull that was skipped) used to bump from its own stale state and
// generate a name that already exists upstream, force-pushing over the remote
// tag. PullTags must surface the upstream tags first, so the bump lands above
// everything the remote carries.
func TestPullTagsAvoidsUpstreamCollision(t *testing.T) {
	bare := newRemoteBare(t, "omcpl")

	// Seed the remote with two commits and two tags, like two previous
	// omc runs on another machine that pushed each of them.
	seed, seedWt, seedDir := initTestRepo(t)
	if _, err := seed.CreateRemote(&config.RemoteConfig{
		Name: "origin",
		URLs: []string{"omcpl://omc/repos/remote.git"},
	}); err != nil {
		t.Fatal(err)
	}
	id := Identity{Name: "Test User", Email: "test@example.com"}
	for _, tag := range []string{"v0.0.1", "v0.0.2"} {
		if err := os.WriteFile(filepath.Join(seedDir, tag+".txt"), []byte(tag), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := seedWt.AddWithOptions(&git.AddOptions{All: true}); err != nil {
			t.Fatal(err)
		}
		h, err := Commit(seed, CommitMessage{Subject: tag})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := CreateTag(seed, h, tag, tag, id); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := PushToRemote(seed, "", sign.TouchConfig{}); err != nil {
		t.Fatalf("seed push: %v", err)
	}

	// The local repository has no history and no tags of its own (a fresh
	// clone by copy): its stale bump from zero would collide with the
	// upstream v0.0.1.
	repo, _, _ := initTestRepo(t)
	if _, err := repo.CreateRemote(&config.RemoteConfig{
		Name: "origin",
		URLs: []string{"omcpl://omc/repos/remote.git"},
	}); err != nil {
		t.Fatal(err)
	}

	if latest, _ := LatestSemverTag(repo); latest != "" {
		t.Fatalf("pre-pull latest tag = %q, want none", latest)
	}
	if next := NextSemverTag(""); next != "v0.0.1" {
		t.Fatalf("pre-pull bump = %q, want the colliding v0.0.1", next)
	}

	// The fix: pull the upstream tag state before generating.
	if err := PullTags(repo, "", sign.TouchConfig{}); err != nil {
		t.Fatalf("PullTags: %v", err)
	}

	latest, err := LatestSemverTag(repo)
	if err != nil {
		t.Fatal(err)
	}
	if latest != "v0.0.2" {
		t.Fatalf("post-pull latest tag = %q, want v0.0.2", latest)
	}
	if next := NextSemverTag(latest); next != "v0.0.3" {
		t.Fatalf("post-pull bump = %q, want v0.0.3", next)
	}

	// The pulled refs must be the remote's, resolving to real tag objects.
	want, err := bare.Storer.Reference(plumbing.NewTagReferenceName("v0.0.2"))
	if err != nil {
		t.Fatalf("remote v0.0.2 missing: %v", err)
	}
	got, err := repo.Storer.Reference(plumbing.NewTagReferenceName("v0.0.2"))
	if err != nil {
		t.Fatalf("local v0.0.2 ref missing after pull: %v", err)
	}
	if got.Hash() != want.Hash() {
		t.Errorf("local v0.0.2 = %s, want remote's %s", got.Hash(), want.Hash())
	}
	if _, err := repo.TagObject(got.Hash()); err != nil {
		t.Errorf("pulled v0.0.2 does not resolve to a tag object: %v", err)
	}

	// The bump must really be free: creating it succeeds instead of
	// colliding. Tag the commit the pulled v0.0.2 points at, like the
	// pipeline tags the new commit.
	tgt, err := repo.TagObject(got.Hash())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := CreateTag(repo, tgt.Target, "v0.0.3", "bumped", id); err != nil {
		t.Fatalf("create bumped tag: %v", err)
	}
}

// TestPullTagsNoRemoteIsSuccess verifies the best-effort contract: a
// repository without a remote has nothing to pull; that is not a failure.
func TestPullTagsNoRemoteIsSuccess(t *testing.T) {
	repo, _, _ := initTestRepo(t)
	if err := PullTags(repo, "", sign.TouchConfig{}); err != nil {
		t.Fatalf("PullTags without remote = %v, want nil", err)
	}
}

// TestPullTagsAlreadyUpToDate verifies a second pull over an unchanged
// remote is a no-op success (NoErrAlreadyUpToDate is treated as done).
func TestPullTagsAlreadyUpToDate(t *testing.T) {
	newRemoteBare(t, "omcpu")
	repo, _, _ := initTestRepo(t)
	if _, err := repo.CreateRemote(&config.RemoteConfig{
		Name: "origin",
		URLs: []string{"omcpu://omc/repos/remote.git"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := PullTags(repo, "", sign.TouchConfig{}); err != nil {
		t.Fatalf("first pull: %v", err)
	}
	if err := PullTags(repo, "", sign.TouchConfig{}); err != nil {
		t.Fatalf("second pull should be up to date, got: %v", err)
	}
}
