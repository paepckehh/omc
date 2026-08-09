package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestMainEndToEnd builds and runs the real binary inside a fresh repo,
// verifying the full flow: staging, commit and log output.
func TestMainEndToEnd(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping binary e2e in short mode")
	}
	bin := filepath.Join(t.TempDir(), "ocommit")
	build := exec.Command("go", "build", "-o", bin, ".")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}

	dir := t.TempDir()
	repoDir := filepath.Join(dir, "repo")
	if err := os.MkdirAll(repoDir, 0o755); err != nil {
		t.Fatal(err)
	}
	init := exec.Command("git", "init", "-q")
	init.Dir = repoDir
	if out, err := init.CombinedOutput(); err != nil {
		t.Skipf("git unavailable: %v\n%s", err, out)
	}
	cfg := exec.Command("git", "config", "user.name", "E2E User")
	cfg.Dir = repoDir
	_ = cfg.Run()
	cfg = exec.Command("git", "config", "user.email", "e2e@example.com")
	cfg.Dir = repoDir
	_ = cfg.Run()

	if err := os.WriteFile(filepath.Join(repoDir, "hello.txt"), []byte("hello e2e\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command(bin)
	cmd.Dir = repoDir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=E2E User",
		"GIT_AUTHOR_EMAIL=e2e@example.com",
		"GIT_COMMITTER_NAME=E2E User",
		"GIT_COMMITTER_EMAIL=e2e@example.com",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("ocommit failed: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "committed") {
		t.Errorf("output missing result: %s", out)
	}
	// The log should show the commit subject line ("update" by default).
	if !strings.Contains(string(out), "update") {
		t.Errorf("output missing commit subject: %s", out)
	}
	// The auto-tag step must announce the semver tag.
	if !strings.Contains(string(out), "tagged") {
		t.Errorf("output missing tag notice: %s", out)
	}

	// Verify the repo actually has a commit.
	show := exec.Command("git", "rev-parse", "HEAD")
	show.Dir = repoDir
	if out2, err := show.CombinedOutput(); err != nil || len(out2) == 0 {
		t.Fatalf("no commit created: %v\n%s", err, out2)
	}

	// Verify the semver tag was created and points at HEAD.
	tag := exec.Command("git", "tag", "-l", "v*")
	tag.Dir = repoDir
	tagOut, err := tag.CombinedOutput()
	if err != nil {
		t.Fatalf("git tag -l failed: %v\n%s", err, tagOut)
	}
	tags := strings.Fields(strings.TrimSpace(string(tagOut)))
	if len(tags) != 1 {
		t.Fatalf("expected 1 semver tag, got %d: %v", len(tags), tags)
	}
	if tags[0] != "v0.0.1" {
		t.Errorf("tag = %q, want v0.0.1", tags[0])
	}
	revTag := exec.Command("git", "rev-parse", tags[0])
	revTag.Dir = repoDir
	revOut, err := revTag.CombinedOutput()
	if err != nil {
		t.Fatalf("git rev-parse %s failed: %v\n%s", tags[0], err, revOut)
	}
	// An annotated tag ref resolves to the tag object, which dereferences
	// to the commit. Use^{commit} to get the commit hash.
	deref := exec.Command("git", "rev-parse", tags[0]+"^{commit}")
	deref.Dir = repoDir
	derefOut, err := deref.CombinedOutput()
	if err != nil {
		t.Fatalf("git rev-parse %s^{{commit}} failed: %v\n%s", tags[0], err, derefOut)
	}
	if strings.TrimSpace(string(derefOut)) != strings.TrimSpace(string(revOut)) {
		// The tag may be annotated; verify it dereferences to HEAD.
		headRev := exec.Command("git", "rev-parse", "HEAD")
		headRev.Dir = repoDir
		headOut, _ := headRev.CombinedOutput()
		if strings.TrimSpace(string(derefOut)) != strings.TrimSpace(string(headOut)) {
			t.Errorf("tag does not point at HEAD: tag=%s head=%s",
				strings.TrimSpace(string(derefOut)), strings.TrimSpace(string(headOut)))
		}
	}
}

// TestMainEndToEndMissingKey verifies that a configured but missing SSH key
// degrades gracefully: the commit still happens, unsigned, with a warning.
func TestMainEndToEndMissingKey(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping binary e2e in short mode")
	}
	bin := filepath.Join(t.TempDir(), "ocommit")
	build := exec.Command("go", "build", "-o", bin, ".")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}

	dir := t.TempDir()
	repoDir := filepath.Join(dir, "repo")
	if err := os.MkdirAll(repoDir, 0o755); err != nil {
		t.Fatal(err)
	}
	init := exec.Command("git", "init", "-q")
	init.Dir = repoDir
	if out, err := init.CombinedOutput(); err != nil {
		t.Skipf("git unavailable: %v\n%s", err, out)
	}

	if err := os.WriteFile(filepath.Join(repoDir, "hello.txt"), []byte("hello e2e\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command(bin)
	cmd.Dir = repoDir
	cmd.Env = append(os.Environ(),
		"OCOMMIT_KEY_PATH="+filepath.Join(dir, "does-not-exist"),
		"OCOMMIT_NAME=E2E User",
		"OCOMMIT_EMAIL=e2e@example.com",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("ocommit failed with missing key: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "warning") {
		t.Errorf("output missing degradation warning: %s", out)
	}
	if !strings.Contains(string(out), "committed") {
		t.Errorf("output missing result: %s", out)
	}
	// Commit must exist and show the env-provided identity.
	show := exec.Command("git", "show", "-s", "--format=%an <%ae>", "HEAD")
	show.Dir = repoDir
	if out2, err := show.CombinedOutput(); err != nil {
		t.Fatalf("no commit created: %v\n%s", err, out2)
	} else if got := strings.TrimSpace(string(out2)); got != "E2E User <e2e@example.com>" {
		t.Errorf("author = %q, want E2E User <e2e@example.com>", got)
	}
}

// TestMainEndToEndCleanTree verifies that when there is nothing to commit,
// ocommit informs the user and exits 0 without creating a commit.
func TestMainEndToEndCleanTree(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping binary e2e in short mode")
	}
	bin := filepath.Join(t.TempDir(), "ocommit")
	build := exec.Command("go", "build", "-o", bin, ".")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}

	dir := t.TempDir()
	repoDir := filepath.Join(dir, "repo")
	if err := os.MkdirAll(repoDir, 0o755); err != nil {
		t.Fatal(err)
	}
	init := exec.Command("git", "init", "-q")
	init.Dir = repoDir
	if out, err := init.CombinedOutput(); err != nil {
		t.Skipf("git unavailable: %v\n%s", err, out)
	}
	// Make one commit so the working tree is clean relative to HEAD.
	if err := os.WriteFile(filepath.Join(repoDir, "hello.txt"), []byte("hello e2e\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := exec.Command("git", "config", "user.name", "E2E User")
	cfg.Dir = repoDir
	_ = cfg.Run()
	cfg = exec.Command("git", "config", "user.email", "e2e@example.com")
	cfg.Dir = repoDir
	_ = cfg.Run()
	commit := exec.Command("git", "add", "-A")
	commit.Dir = repoDir
	_ = commit.Run()
	commit = exec.Command("git", "commit", "-q", "-m", "seed")
	commit.Dir = repoDir
	if out, err := commit.CombinedOutput(); err != nil {
		t.Fatalf("seed commit failed: %v\n%s", err, out)
	}

	cmd := exec.Command(bin)
	cmd.Dir = repoDir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=E2E User",
		"GIT_AUTHOR_EMAIL=e2e@example.com",
		"GIT_COMMITTER_NAME=E2E User",
		"GIT_COMMITTER_EMAIL=e2e@example.com",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("ocommit failed on clean tree: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "nothing to commit") {
		t.Errorf("output missing clean-tree notice: %s", out)
	}

	// No new commit may be created.
	show := exec.Command("git", "rev-list", "--count", "HEAD")
	show.Dir = repoDir
	out2, err := show.CombinedOutput()
	if err != nil {
		t.Fatalf("rev-list failed: %v\n%s", err, out2)
	}
	if got := strings.TrimSpace(string(out2)); got != "1" {
		t.Errorf("commit count = %s, want 1 (no new commit)", got)
	}
}
