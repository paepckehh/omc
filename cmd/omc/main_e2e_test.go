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
	bin := filepath.Join(t.TempDir(), "omc")
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
		t.Fatalf("omc failed: %v\n%s", err, out)
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
	bin := filepath.Join(t.TempDir(), "omc")
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
		"OMC_SIGN_KEY_PATH="+filepath.Join(dir, "does-not-exist"),
		"OMC_NAME=E2E User",
		"OMC_EMAIL=e2e@example.com",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("omc failed with missing key: %v\n%s", err, out)
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

// TestMainEndToEndOverrides verifies that OMC_SUBJECT, OMC_MESSAGE
// and a valid OMC_TAG override the generated message and tag, and that
// no Ollama call is attempted (the override message lands verbatim).
func TestMainEndToEndOverrides(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping binary e2e in short mode")
	}
	bin := filepath.Join(t.TempDir(), "omc")
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

	if err := os.WriteFile(filepath.Join(repoDir, "hello.txt"), []byte("override e2e\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command(bin)
	cmd.Dir = repoDir
	cmd.Env = append(os.Environ(),
		"OMC_SUBJECT=feat: override subject",
		"OMC_MESSAGE=multi\nline\nbody",
		"OMC_TAG=v3.2.1",
		"OMC_NAME=E2E User",
		"OMC_EMAIL=e2e@example.com",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("omc failed: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "override subject") {
		t.Errorf("output missing override subject: %s", out)
	}
	if !strings.Contains(string(out), "v3.2.1") {
		t.Errorf("output missing override tag: %s", out)
	}

	// The override tag must be the only tag created.
	tag := exec.Command("git", "tag", "-l", "v*")
	tag.Dir = repoDir
	tagOut, err := tag.CombinedOutput()
	if err != nil {
		t.Fatalf("git tag -l failed: %v\n%s", err, tagOut)
	}
	tags := strings.Fields(strings.TrimSpace(string(tagOut)))
	if len(tags) != 1 || tags[0] != "v3.2.1" {
		t.Errorf("tags = %v, want [v3.2.1]", tags)
	}

	// The commit subject must be the override subject.
	show := exec.Command("git", "show", "-s", "--format=%s", "HEAD")
	show.Dir = repoDir
	if subj, err := show.CombinedOutput(); err != nil {
		t.Fatalf("git show: %v", err)
	} else if got := strings.TrimSpace(string(subj)); got != "feat: override subject" {
		t.Errorf("subject = %q, want feat: override subject", got)
	}

	// The body must be the override message.
	body := exec.Command("git", "show", "-s", "--format=%b", "HEAD")
	body.Dir = repoDir
	if b, err := body.CombinedOutput(); err != nil {
		t.Fatalf("git show body: %v", err)
	} else if got := strings.TrimSpace(string(b)); got != "multi\nline\nbody" {
		t.Errorf("body = %q, want multi\\nline\\nbody", got)
	}
}

// TestMainEndToEndSubjectOnly verifies that OMC_SUBJECT alone is used for
// both subject and body.
func TestMainEndToEndSubjectOnly(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping binary e2e in short mode")
	}
	bin := filepath.Join(t.TempDir(), "omc")
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

	if err := os.WriteFile(filepath.Join(repoDir, "hello.txt"), []byte("subject only\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command(bin)
	cmd.Dir = repoDir
	cmd.Env = append(os.Environ(),
		"OMC_SUBJECT=solo subject",
		"GIT_AUTHOR_NAME=E2E User",
		"GIT_AUTHOR_EMAIL=e2e@example.com",
		"GIT_COMMITTER_NAME=E2E User",
		"GIT_COMMITTER_EMAIL=e2e@example.com",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("omc failed: %v\n%s", err, out)
	}

	show := exec.Command("git", "show", "-s", "--format=%s", "HEAD")
	show.Dir = repoDir
	if subj, err := show.CombinedOutput(); err != nil {
		t.Fatalf("git show: %v", err)
	} else if got := strings.TrimSpace(string(subj)); got != "solo subject" {
		t.Errorf("subject = %q, want solo subject", got)
	}

	// Body must equal the subject when only OMC_SUBJECT is set.
	body := exec.Command("git", "show", "-s", "--format=%b", "HEAD")
	body.Dir = repoDir
	if b, err := body.CombinedOutput(); err != nil {
		t.Fatalf("git show body: %v", err)
	} else if got := strings.TrimSpace(string(b)); got != "solo subject" {
		t.Errorf("body = %q, want solo subject (same as subject)", got)
	}
}

// TestMainEndToEndMessageOnly verifies that OMC_MESSAGE alone is used as
// the body, with its first line shortened into the subject.
func TestMainEndToEndMessageOnly(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping binary e2e in short mode")
	}
	bin := filepath.Join(t.TempDir(), "omc")
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

	if err := os.WriteFile(filepath.Join(repoDir, "hello.txt"), []byte("message only\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command(bin)
	cmd.Dir = repoDir
	cmd.Env = append(os.Environ(),
		"OMC_MESSAGE=first line of body\nsecond line of body",
		"GIT_AUTHOR_NAME=E2E User",
		"GIT_AUTHOR_EMAIL=e2e@example.com",
		"GIT_COMMITTER_NAME=E2E User",
		"GIT_COMMITTER_EMAIL=e2e@example.com",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("omc failed: %v\n%s", err, out)
	}

	show := exec.Command("git", "show", "-s", "--format=%s", "HEAD")
	show.Dir = repoDir
	if subj, err := show.CombinedOutput(); err != nil {
		t.Fatalf("git show: %v", err)
	} else if got := strings.TrimSpace(string(subj)); got != "first line of body" {
		t.Errorf("subject = %q, want first line of body", got)
	}

	body := exec.Command("git", "show", "-s", "--format=%b", "HEAD")
	body.Dir = repoDir
	if b, err := body.CombinedOutput(); err != nil {
		t.Fatalf("git show body: %v", err)
	} else if got := strings.TrimSpace(string(b)); got != "first line of body\nsecond line of body" {
		t.Errorf("body = %q, want the full message", got)
	}
}

// TestMainEndToEndInvalidTagOverride verifies that an OMC_TAG that does
// not parse as strict semver is ignored and the normal auto-bump path runs
// (v0.0.1 on a fresh repo).
func TestMainEndToEndInvalidTagOverride(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping binary e2e in short mode")
	}
	bin := filepath.Join(t.TempDir(), "omc")
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

	if err := os.WriteFile(filepath.Join(repoDir, "hello.txt"), []byte("bad tag\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command(bin)
	cmd.Dir = repoDir
	cmd.Env = append(os.Environ(),
		"OMC_TAG=not-semver",
		"GIT_AUTHOR_NAME=E2E User",
		"GIT_AUTHOR_EMAIL=e2e@example.com",
		"GIT_COMMITTER_NAME=E2E User",
		"GIT_COMMITTER_EMAIL=e2e@example.com",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("omc failed: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "warning") {
		t.Errorf("output missing invalid-tag warning: %s", out)
	}

	tag := exec.Command("git", "tag", "-l", "v*")
	tag.Dir = repoDir
	tagOut, err := tag.CombinedOutput()
	if err != nil {
		t.Fatalf("git tag -l failed: %v\n%s", err, tagOut)
	}
	tags := strings.Fields(strings.TrimSpace(string(tagOut)))
	if len(tags) != 1 || tags[0] != "v0.0.1" {
		t.Errorf("tags = %v, want [v0.0.1] (auto-bump after invalid override)", tags)
	}
}

// TestMainEndToEndCleanTree verifies that when there is nothing to commit,
// omc informs the user and exits 0 without creating a commit.
func TestMainEndToEndCleanTree(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping binary e2e in short mode")
	}
	bin := filepath.Join(t.TempDir(), "omc")
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
		t.Fatalf("omc failed on clean tree: %v\n%s", err, out)
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

// TestMainEndToEndSignedTag verifies task (b): when a valid
// OMC_SIGN_KEY_PATH is configured, the auto-bumped semver tag (v0.0.1) must
// ALWAYS be SSH-signed with the same key used for the commit. The signature
// is verified with `git tag -v`, which accepts go-git's byte-conformant
// signed tag objects.
func TestMainEndToEndSignedTag(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping binary e2e in short mode")
	}
	bin := filepath.Join(t.TempDir(), "omc")
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

	// Generate a fresh, unencrypted ed25519 key.
	keygen := exec.Command("ssh-keygen", "-t", "ed25519", "-N", "", "-f", filepath.Join(dir, "id"), "-q")
	if out, err := keygen.CombinedOutput(); err != nil {
		t.Skipf("ssh-keygen unavailable: %v\n%s", err, out)
	}
	keyPath := filepath.Join(dir, "id")

	// Configure git to verify SSH signatures against the generated public
	// key, so `git tag -v` can actually validate the tag.
	pub, err := os.ReadFile(keyPath + ".pub")
	if err != nil {
		t.Fatal(err)
	}
	allowed := filepath.Join(dir, "allowed_signers")
	if err := os.WriteFile(allowed, []byte("e2e@example.com "+strings.TrimSpace(string(pub))+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, kv := range []string{
		"gpg.format ssh",
		"gpg.ssh.allowedSignersFile " + allowed,
	} {
		parts := strings.SplitN(kv, " ", 2)
		cfg := exec.Command("git", "config", parts[0], parts[1])
		cfg.Dir = repoDir
		if out, err := cfg.CombinedOutput(); err != nil {
			t.Fatalf("git config %s: %v\n%s", kv, err, out)
		}
	}

	if err := os.WriteFile(filepath.Join(repoDir, "hello.txt"), []byte("signed tag e2e\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command(bin)
	cmd.Dir = repoDir
	cmd.Env = append(os.Environ(),
		"OMC_SIGN_KEY_PATH="+keyPath,
		"OMC_NAME=E2E User",
		"OMC_EMAIL=e2e@example.com",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("omc failed: %v\n%s", err, out)
	}

	// The tag result must report signed=true.
	if !strings.Contains(string(out), "signed=true") {
		t.Errorf("tag not reported as signed: %s", out)
	}

	// The tag object must carry an armored SSH signature, whose validity
	// git accepts.
	tagv := exec.Command("git", "tag", "-v", "v0.0.1")
	tagv.Dir = repoDir
	tagOut, err := tagv.CombinedOutput()
	if err != nil {
		t.Fatalf("git tag -v v0.0.1 rejected signature: %v\n%s", err, tagOut)
	}
	if !strings.Contains(string(tagOut), "Good") {
		t.Errorf("signature not verified: %s", tagOut)
	}
}
