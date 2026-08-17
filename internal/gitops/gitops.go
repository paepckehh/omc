// Package gitops implements the git operations behind omc: repository
// detection, staging, diffs, committing and history output. All of it is
// done through the go-git library, so the tool has no runtime dependency on
// a git executable.
package gitops

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/filemode"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/transport"
	sshpkg "github.com/go-git/go-git/v5/plumbing/transport/ssh"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"

	"paepcke.de/omc/internal/sign"
)

// ErrNotARepository is returned when the current directory is not inside a
// git working tree.
var ErrNotARepository = fmt.Errorf("not a git repository")

// Open opens the repository enclosing the working directory, mirroring how
// git discovers .git by walking up parent directories.
func Open() (*git.Repository, *git.Worktree, error) {
	repo, err := git.PlainOpenWithOptions(".", &git.PlainOpenOptions{
		DetectDotGit: true,
	})
	if err != nil {
		if err == git.ErrRepositoryNotExists {
			return nil, nil, ErrNotARepository
		}
		return nil, nil, err
	}
	wt, err := repo.Worktree()
	if err != nil {
		return nil, nil, fmt.Errorf("worktree: %w", err)
	}
	return repo, wt, nil
}

// RepoContext holds the diagnostic metadata about the opened repository shown
// right after the open step succeeds: the containing directory (the worktree
// root, where .git lives), the configured remotes (name → URLs, used for both
// fetch and push unless a remote configures separate push URLs), and the
// highest semver tag detected. Each field is best-effort: when something
// cannot be read it is left empty and the caller emits a "(none)" placeholder.
type RepoContext struct {
	// Dir is the worktree root path (the containing directory for .git).
	Dir string
	// Remotes maps each configured remote name to its URL list. go-git stores
	// the fetch URLs in RemoteConfig.URLs; the first URL is used for fetch
	// and all of them are tried for push (matching git's behavior when no
	// separate pushurl is set).
	Remotes map[string][]string
	// LatestTag is the highest semver tag name (with leading "v"), or empty
	// when no semver tag exists.
	LatestTag string
}

// Scout gathers the repository context metadata for the startup diagnostic
// shown right after the open step. It never returns an error: every piece is
// best-effort, and a failure reading one field leaves it empty so the caller
// can emit a "(none)" placeholder without aborting the pipeline.
func Scout(repo *git.Repository, wt *git.Worktree) RepoContext {
	ctx := RepoContext{Remotes: map[string][]string{}}
	if wt != nil && wt.Filesystem != nil {
		ctx.Dir = wt.Filesystem.Root()
	}
	if repo != nil {
		if cfg, err := repo.Config(); err == nil {
			for name, r := range cfg.Remotes {
				if len(r.URLs) > 0 {
					ctx.Remotes[name] = r.URLs
				}
			}
		}
		ctx.LatestTag, _ = LatestSemverTag(repo)
	}
	return ctx
}

// StageAll performs the equivalent of "git add -A": every tracked, modified
// and untracked file in the working tree is added to the index, including
// deletions.
func StageAll(wt *git.Worktree) error {
	if err := wt.AddWithOptions(&git.AddOptions{All: true}); err != nil {
		return fmt.Errorf("git add -A: %w", err)
	}
	return nil
}

// StagedDiff returns the patch text that will be committed: the difference
// between HEAD's tree and the current index, as a unified diff. On the root
// commit the diff covers the full tree. The returned fileNames slice lists
// the paths touched (post-rename names), suitable as a compact summary for
// an LLM prompt; it is empty when nothing changed.
func StagedDiff(repo *git.Repository, wt *git.Worktree) (diff string, fileNames []string, err error) {
	var from *object.Tree
	if ref, e := repo.Head(); e == nil {
		c, e := repo.CommitObject(ref.Hash())
		if e != nil {
			return "", nil, fmt.Errorf("read HEAD commit: %w", e)
		}
		from, e = c.Tree()
		if e != nil {
			return "", nil, fmt.Errorf("read HEAD tree: %w", e)
		}
	}

	toHash, err := writeIndexTree(repo)
	if err != nil {
		return "", nil, err
	}
	to, err := repo.TreeObject(toHash)
	if err != nil {
		return "", nil, fmt.Errorf("read index tree: %w", err)
	}

	changes, err := object.DiffTreeWithOptions(
		context.Background(), from, to,
		&object.DiffTreeOptions{DetectRenames: true},
	)
	if err != nil {
		return "", nil, fmt.Errorf("diff HEAD against index: %w", err)
	}

	patch, err := changes.Patch()
	if err != nil {
		return "", nil, fmt.Errorf("build patch: %w", err)
	}
	for _, ch := range changes {
		if ch == nil {
			continue
		}
		name := ch.To.Name
		if name == "" {
			name = ch.From.Name
		}
		if name != "" {
			fileNames = append(fileNames, name)
		}
	}
	return patch.String(), fileNames, nil
}

// CommitMessage holds the final subject and body of a commit. The subject is
// the shortened TL;DR; the body carries the full LLM-generated description.
type CommitMessage struct {
	// Subject is the first line of the commit message.
	Subject string
	// Body is the remainder of the commit message, may be empty.
	Body string
}

// defaults for the commit identity when nothing else is configured.
const (
	DefaultName  = "OMC, Git Commiter"
	DefaultEmail = "git@omc.local"
)

// Identity is the resolved author/committer for a commit.
type Identity struct {
	Name  string
	Email string
}

// ResolveIdentity returns the commit identity: OMC_NAME/OMC_EMAIL
// first, then the standard GIT_AUTHOR_*/GIT_COMMITTER_* variables, then the
// repository's git config, and finally the built-in defaults.
// ResolveIdentity returns the commit identity: OMC_NAME/OMC_EMAIL
// first, then the standard GIT_AUTHOR_*/GIT_COMMITTER_* variables, then the
// repository's git config, and finally the built-in defaults.
func ResolveIdentity(repo *git.Repository) Identity {
	sig := Identity{Name: DefaultName, Email: DefaultEmail}
	nameEnv := envOr("OMC_NAME", "GIT_AUTHOR_NAME", "GIT_COMMITTER_NAME")
	emailEnv := envOr("OMC_EMAIL", "GIT_AUTHOR_EMAIL", "GIT_COMMITTER_EMAIL")
	if nameEnv != "" {
		sig.Name = nameEnv
	}
	if emailEnv != "" {
		sig.Email = emailEnv
	}
	if repo != nil && (nameEnv == "" || emailEnv == "") {
		if cfg, err := repo.Config(); err == nil {
			if nameEnv == "" && cfg.User.Name != "" {
				sig.Name = cfg.User.Name
			}
			if emailEnv == "" && cfg.User.Email != "" {
				sig.Email = cfg.User.Email
			}
		}
	}
	return sig
}

// identity reads the identity from the environment and, failing that, from
// the repository's git config, and finally falls back to fixed defaults.

// envOr returns the first non-empty environment variable among keys.
func envOr(keys ...string) string {
	for _, k := range keys {
		if v := os.Getenv(k); v != "" {
			return v
		}
	}
	return ""
}

// signature builds an author/committer signature from a resolved identity.
func signatureFrom(id Identity) object.Signature {
	return object.Signature{
		Name:  id.Name,
		Email: id.Email,
		When:  time.Now(),
	}
}

// String renders the canonical commit message text.
func (m CommitMessage) String() string {
	s := strings.TrimSpace(m.Subject)
	b := strings.TrimSpace(m.Body)
	switch {
	case s == "" && b == "":
		return ""
	case b == "":
		return s
	default:
		return s + "\n\n" + b
	}
}

// commitSubject returns the canonical message, falling back to "update" when
// the resolved message is empty.
func (m CommitMessage) commitSubject() string {
	if s := m.String(); s != "" {
		return s
	}
	return "update"
}

// parentsOf returns the parent commit hashes for a new commit: HEAD's hash
// when the repository already has history, or none for the root commit.
func parentsOf(repo *git.Repository) ([]plumbing.Hash, error) {
	head, err := repo.Head()
	switch err {
	case nil:
		return []plumbing.Hash{head.Hash()}, nil
	case plumbing.ErrReferenceNotFound:
		return nil, nil
	default:
		return nil, fmt.Errorf("read HEAD: %w", err)
	}
}

// Commit creates a new commit from the current index, using the given
// message, and advances the current branch. It returns the new commit's
// hash. When msg is empty the subject falls back to "update".
func Commit(repo *git.Repository, wt *git.Worktree, msg CommitMessage) (plumbing.Hash, error) {
	treeHash, err := writeIndexTree(repo)
	if err != nil {
		return plumbing.ZeroHash, err
	}
	parents, err := parentsOf(repo)
	if err != nil {
		return plumbing.ZeroHash, err
	}

	sig := signatureFrom(ResolveIdentity(repo))
	commit := &object.Commit{
		Author:       sig,
		Committer:    sig,
		Message:      msg.commitSubject(),
		TreeHash:     treeHash,
		ParentHashes: parents,
	}

	obj := repo.Storer.NewEncodedObject()
	if err := commit.Encode(obj); err != nil {
		return plumbing.ZeroHash, fmt.Errorf("encode commit: %w", err)
	}
	hash, err := repo.Storer.SetEncodedObject(obj)
	if err != nil {
		return plumbing.ZeroHash, fmt.Errorf("store commit: %w", err)
	}

	if err := advanceBranch(repo, hash); err != nil {
		return plumbing.ZeroHash, err
	}
	return hash, nil
}

// SignedCommit creates a commit like Commit but embeds an armored SSH
// signature into the commit object, mirroring how git signs commits with
// "git commit -S" and the ssh format. The signature covers exactly the
// header bytes git signs (tree/parent/author/committer lines); the message
// is not part of the signed payload, just as in git.
func SignedCommit(repo *git.Repository, wt *git.Worktree, msg CommitMessage, armoredSign func([]byte) ([]byte, error)) (plumbing.Hash, error) {
	treeHash, err := writeIndexTree(repo)
	if err != nil {
		return plumbing.ZeroHash, err
	}
	parents, err := parentsOf(repo)
	if err != nil {
		return plumbing.ZeroHash, err
	}

	sig := signatureFrom(ResolveIdentity(repo))

	// Header block: identical to what git signs for a signed commit.
	var headers bytes.Buffer
	fmt.Fprintf(&headers, "tree %s\n", treeHash)
	for _, p := range parents {
		fmt.Fprintf(&headers, "parent %s\n", p)
	}
	headers.WriteString("author ")
	if err := sig.Encode(&headers); err != nil {
		return plumbing.ZeroHash, fmt.Errorf("encode author: %w", err)
	}
	headers.WriteString("\ncommitter ")
	if err := sig.Encode(&headers); err != nil {
		return plumbing.ZeroHash, fmt.Errorf("encode committer: %w", err)
	}
	headers.WriteString("\n")
	payload := headers.String()

	armored, err := armoredSign([]byte(payload))
	if err != nil {
		return plumbing.ZeroHash, err
	}

	// Stored object: headers, then "gpgsig" + armor with continuation
	// spaces on every line, then blank line, then the message. git requires
	// the commit body to end with a newline.
	message := msg.commitSubject()
	if !strings.HasSuffix(message, "\n") {
		message += "\n"
	}

	var final strings.Builder
	final.WriteString(payload)
	armor := strings.TrimSuffix(string(armored), "\n")
	final.WriteString("gpgsig ")
	final.WriteString(strings.ReplaceAll(armor, "\n", "\n "))
	final.WriteString("\n\n")
	final.WriteString(message)

	storageObj := repo.Storer.NewEncodedObject()
	storageObj.SetType(plumbing.CommitObject)
	storageObj.SetSize(int64(final.Len()))
	w, err := storageObj.Writer()
	if err != nil {
		return plumbing.ZeroHash, fmt.Errorf("open writer: %w", err)
	}
	if _, err := w.Write([]byte(final.String())); err != nil {
		w.Close()
		return plumbing.ZeroHash, fmt.Errorf("write signed commit: %w", err)
	}
	if err := w.Close(); err != nil {
		return plumbing.ZeroHash, fmt.Errorf("close writer: %w", err)
	}
	hash, err := repo.Storer.SetEncodedObject(storageObj)
	if err != nil {
		return plumbing.ZeroHash, fmt.Errorf("store commit: %w", err)
	}

	if err := advanceBranch(repo, hash); err != nil {
		return plumbing.ZeroHash, err
	}
	return hash, nil
}

// advanceBranch moves the current branch (or detached HEAD) to the new
// commit hash, mirroring what "git commit" does at the end.
func advanceBranch(repo *git.Repository, hash plumbing.Hash) error {
	head, err := repo.Storer.Reference(plumbing.HEAD)
	if err != nil {
		return fmt.Errorf("read HEAD ref: %w", err)
	}
	if head.Type() != plumbing.SymbolicReference {
		// Detached HEAD.
		return repo.Storer.SetReference(plumbing.NewHashReference(plumbing.HEAD, hash))
	}
	return repo.Storer.SetReference(plumbing.NewHashReference(head.Target(), hash))
}

// writeIndexTree writes a tree object matching the current index to the
// object database and returns its hash.
func writeIndexTree(repo *git.Repository) (plumbing.Hash, error) {
	idx, err := repo.Storer.Index()
	if err != nil {
		return plumbing.ZeroHash, fmt.Errorf("read index: %w", err)
	}

	nodes := map[string]*indexNode{}
	var kids []*indexNode
	seen := map[string]bool{}
	for _, e := range idx.Entries {
		parts := strings.Split(e.Name, "/")
		path := ""
		for n, part := range parts {
			prev := path
			if path == "" {
				path = part
			} else {
				path = path + "/" + part
			}
			node, ok := nodes[path]
			if !ok {
				node = &indexNode{name: part, children: map[string]*indexNode{}}
				nodes[path] = node
				if prev != "" {
					nodes[prev].children[part] = node
				} else if !seen[part] {
					seen[part] = true
					kids = append(kids, node)
				}
			}
			if n == len(parts)-1 {
				node.hash = e.Hash
				node.mode = e.Mode
			}
		}
	}

	root := &object.Tree{}
	return walkTree(repo, root, kids)
}

type indexNode struct {
	name     string
	hash     plumbing.Hash
	mode     filemode.FileMode
	children map[string]*indexNode
}

// walkTree writes tree objects to the storer recursively and returns the
// root hash.
func walkTree(repo *git.Repository, tree *object.Tree, kids []*indexNode) (plumbing.Hash, error) {
	sort.Slice(kids, func(i, j int) bool {
		return indexOrder(kids[i]) < indexOrder(kids[j])
	})
	for _, k := range kids {
		if !k.hash.IsZero() && k.mode != 0 {
			tree.Entries = append(tree.Entries, object.TreeEntry{
				Name: k.name,
				Mode: k.mode,
				Hash: k.hash,
			})
			continue
		}
		sub := &object.Tree{}
		var subKids []*indexNode
		for _, c := range k.children {
			subKids = append(subKids, c)
		}
		subHash, err := walkTree(repo, sub, subKids)
		if err != nil {
			return plumbing.ZeroHash, err
		}
		tree.Entries = append(tree.Entries, object.TreeEntry{
			Name: k.name,
			Mode: filemode.Dir,
			Hash: subHash,
		})
	}

	obj := repo.Storer.NewEncodedObject()
	if err := tree.Encode(obj); err != nil {
		return plumbing.ZeroHash, fmt.Errorf("encode tree: %w", err)
	}
	return repo.Storer.SetEncodedObject(obj)
}

// indexOrder returns the sort key used by git for tree entries: directories
// sort with a trailing slash.
func indexOrder(n *indexNode) string {
	if n.hash.IsZero() {
		return n.name + "/"
	}
	return n.name
}

// --- Semver auto-tagging -----------------------------------------------------

// semverRe matches tags of the form vMAJOR.MINOR.PATCH with an optional
// leading "v", where each numeric component has no leading zeros (except a
// lone 0). Pre-release suffixes (e.g. -rc.1) and build metadata (+build) are
// ignored for patch bumping: the base version still determines the next tag.
var semverRe = regexp.MustCompile(`^v?(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)`)

// strictSemverRe matches an entire string of the form vMAJOR.MINOR.PATCH
// (leading "v" optional) where each numeric component has no leading zeros
// except a lone 0. Pre-release suffixes and build metadata are rejected:
// an override tag must be a clean base semver. Any magnitude is accepted, so
// large jumps in any of the three segments (e.g. v999.0.0) are valid.
var strictSemverRe = regexp.MustCompile(`^v?(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)$`)

// ValidSemverTag reports whether name is a strict vMAJOR.MINOR.PATCH semver
// tag (an optional leading "v", three non-negative components without
// leading zeros, no pre-release/build suffix). It accepts arbitrarily large
// values in all three segments.
func ValidSemverTag(name string) bool {
	return strictSemverRe.MatchString(name)
}

// NormalizeTag returns name with the conventional leading "v" added when it
// is a bare MAJOR.MINOR.PATCH, so an override like "0.1.2" becomes "v0.1.2".
// A name that already starts with "v" is returned unchanged. Names that do
// not parse as strict semver are returned unchanged as well; callers are
// expected to gate this on ValidSemverTag first.
func NormalizeTag(name string) string {
	if strings.HasPrefix(name, "v") {
		return name
	}
	if strictSemverRe.MatchString(name) {
		return "v" + name
	}
	return name
}

// LatestSemverTag scans all refs under refs/tags/ and returns the highest
// semver tag name (including the leading "v"). When no semver tag exists it
// returns "" and a nil error; the caller then bumps the zero version.
func LatestSemverTag(repo *git.Repository) (string, error) {
	iter, err := repo.Tags()
	if err != nil {
		return "", fmt.Errorf("list tags: %w", err)
	}
	defer iter.Close()

	var best *semver
	var bestRaw string
	err = iter.ForEach(func(ref *plumbing.Reference) error {
		name := strings.TrimPrefix(ref.Name().String(), "refs/tags/")
		m := semverRe.FindStringSubmatch(name)
		if m == nil {
			return nil
		}
		v := parseSemver(m[1], m[2], m[3])
		if best == nil || semverLess(best, v) {
			best = v
			bestRaw = name
		}
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("iterate tags: %w", err)
	}
	return bestRaw, nil
}

// NextSemverTag returns the next patch-bumped semver tag for the given latest
// tag. An empty latest means no prior tag exists; the result is v0.0.1.
func NextSemverTag(latest string) string {
	m := semverRe.FindStringSubmatch(latest)
	if m == nil {
		return "v0.0.1"
	}
	major, _ := strconv.Atoi(m[1])
	minor, _ := strconv.Atoi(m[2])
	patch, _ := strconv.Atoi(m[3])
	return fmt.Sprintf("v%d.%d.%d", major, minor, patch+1)
}

// CreateTag creates an annotated tag pointing at hash, using message as the
// tag message and id as the tagger identity. It returns the created reference.
// CreateTag creates an annotated tag pointing at hash, using message as the
// tag message and id as the tagger identity. It returns the created reference.
func CreateTag(repo *git.Repository, hash plumbing.Hash, name, message string, id Identity) (*plumbing.Reference, error) {
	if err := ensureTagAbsent(repo, name); err != nil {
		return nil, err
	}
	payload, err := tagPayload(name, hash, message, id)
	if err != nil {
		return nil, err
	}
	tagHash, err := storeTagObject(repo, payload)
	if err != nil {
		return nil, err
	}
	return setTagRef(repo, name, tagHash)
}

// SignedTag creates an annotated tag like CreateTag but embeds an armored SSH
// signature, mirroring "git tag -s" with the ssh format. The signature covers
// the full tag object content (object/type/tag/tagger/message) exactly as git
// signs it; the armored block is appended after the message, which is how git
// stores signed tags. The payload is built once and both signed and stored, so
// the signature always matches the stored object byte-for-byte.
func SignedTag(repo *git.Repository, hash plumbing.Hash, name, message string, id Identity, armoredSign func([]byte) ([]byte, error)) (*plumbing.Reference, error) {
	if err := ensureTagAbsent(repo, name); err != nil {
		return nil, err
	}
	payload, err := tagPayload(name, hash, message, id)
	if err != nil {
		return nil, err
	}
	armored, err := armoredSign(payload)
	if err != nil {
		return nil, fmt.Errorf("sign tag payload: %w", err)
	}
	sig := strings.TrimSuffix(string(armored), "\n")
	content := append(payload, []byte(sig+"\n")...)
	tagHash, err := storeTagObject(repo, content)
	if err != nil {
		return nil, err
	}
	return setTagRef(repo, name, tagHash)
}

// setTagRef creates the refs/tags/<name> reference pointing at tagHash.
func setTagRef(repo *git.Repository, name string, tagHash plumbing.Hash) (*plumbing.Reference, error) {
	ref := plumbing.NewHashReference(plumbing.NewTagReferenceName(name), tagHash)
	if err := repo.Storer.SetReference(ref); err != nil {
		return nil, fmt.Errorf("set tag ref %s: %w", name, err)
	}
	return ref, nil
}

// ensureTagAbsent returns an error when the tag already exists.
func ensureTagAbsent(repo *git.Repository, name string) error {
	_, err := repo.Storer.Reference(plumbing.NewTagReferenceName(name))
	switch err {
	case nil:
		return fmt.Errorf("tag %s already exists", name)
	case plumbing.ErrReferenceNotFound:
		return nil
	default:
		return fmt.Errorf("check tag %s: %w", name, err)
	}
}

// tagPayload returns the canonical bytes a signed tag covers: everything in
// the tag object before the signature block.
func tagPayload(name string, target plumbing.Hash, message string, id Identity) ([]byte, error) {
	sig := signatureFrom(id)
	var b bytes.Buffer
	fmt.Fprintf(&b, "object %s\n", target)
	fmt.Fprintf(&b, "type %s\n", plumbing.CommitObject.Bytes())
	fmt.Fprintf(&b, "tag %s\n", name)
	b.WriteString("tagger ")
	if err := sig.Encode(&b); err != nil {
		return nil, fmt.Errorf("encode tagger: %w", err)
	}
	b.WriteString("\n\n")
	msg := strings.TrimSpace(message)
	if msg == "" {
		msg = "update"
	}
	b.WriteString(msg)
	b.WriteString("\n")
	return b.Bytes(), nil
}

// buildTagObject assembles the full tag object content, optionally with a
// trailing armored signature block.

// storeTagObject writes raw tag object bytes into the object database and
// returns the new object's hash.
func storeTagObject(repo *git.Repository, content []byte) (plumbing.Hash, error) {
	obj := repo.Storer.NewEncodedObject()
	obj.SetType(plumbing.TagObject)
	obj.SetSize(int64(len(content)))
	w, err := obj.Writer()
	if err != nil {
		return plumbing.ZeroHash, fmt.Errorf("open tag writer: %w", err)
	}
	if _, err := w.Write(content); err != nil {
		w.Close()
		return plumbing.ZeroHash, fmt.Errorf("write tag object: %w", err)
	}
	if err := w.Close(); err != nil {
		return plumbing.ZeroHash, fmt.Errorf("close tag writer: %w", err)
	}
	hash, err := repo.Storer.SetEncodedObject(obj)
	if err != nil {
		return plumbing.ZeroHash, fmt.Errorf("store tag object: %w", err)
	}
	return hash, nil
}

// --- semver helpers ----------------------------------------------------------

type semver struct {
	major, minor, patch int
}

func parseSemver(major, minor, patch string) *semver {
	return &semver{
		major: mustAtoi(major),
		minor: mustAtoi(minor),
		patch: mustAtoi(patch),
	}
}

func mustAtoi(s string) int {
	v, _ := strconv.Atoi(s)
	return v
}

// semverLess reports whether a is strictly older than b.
func semverLess(a, b *semver) bool {
	if a.major != b.major {
		return a.major < b.major
	}
	if a.minor != b.minor {
		return a.minor < b.minor
	}
	return a.patch < b.patch
}

// --- Push to the remote -------------------------------------------------------

// PushResult describes the outcome of a push.
type PushResult struct {
	// Remote is the remote name that was pushed to.
	Remote string
	// Branch is the local branch that was pushed ("" on detached HEAD).
	Branch string
	// Tags reports whether the tags were pushed.
	Tags bool
}

// ErrNoRemoteConfigured is returned when the repository has no remote to
// push to.
var ErrNoRemoteConfigured = fmt.Errorf("no remote configured")

// PushToRemote pushes the current branch and all local tags to the
// repository's default remote, the go-git equivalent of "git push; git
// push --tags". keyPath is an optional SSH private key used for
// authentication; when empty, go-git falls back to its default auth (SSH
// agent). For file:// or https URLs the key is not applicable.
//
// NoErrAlreadyUpToDate is treated as success (nothing to push).
// ErrForceNeeded / non-fast-forward errors from the tag pass are returned
// as errors; the branch push itself already succeeded at that point.
func PushToRemote(repo *git.Repository, keyPath string) (PushResult, error) {
	cfg, err := repo.Config()
	if err != nil {
		return PushResult{}, fmt.Errorf("read remote config: %w", err)
	}
	if len(cfg.Remotes) == 0 {
		return PushResult{}, ErrNoRemoteConfigured
	}

	// Prefer "origin", like git; otherwise use the first configured
	// remote. The default branch refspec is resolved by go-git
	// (refs/heads/*:refs/heads/*).
	remoteName := git.DefaultRemoteName
	if _, ok := cfg.Remotes[remoteName]; !ok {
		for name := range cfg.Remotes {
			remoteName = name
			break
		}
	}
	remoteCfg := cfg.Remotes[remoteName]

	// Keep the remote config in scope: the Remote abstraction is
	// constructed for each pass below, so the config object must stay
	// alive between pushes. Reading the branch from HEAD names the
	// explicit refspec for the first pass.
	branch := ""
	if ref, err := repo.Head(); err == nil && ref.Name().IsBranch() {
		branch = ref.Name().Short()
	}

	auth, err := getSSHAuth(keyPath, remoteCfg.URLs)
	if err != nil {
		return PushResult{}, err
	}

	opts := &git.PushOptions{RemoteName: remoteName, Auth: auth}
	if branch != "" {
		opts.RefSpecs = []config.RefSpec{
			config.RefSpec("refs/heads/" + branch + ":refs/heads/" + branch),
		}
	}
	if err := pushOnce(repo, remoteCfg, opts); err != nil {
		if errors.Is(err, git.NoErrAlreadyUpToDate) {
			return PushResult{Remote: remoteName, Branch: branch, Tags: false}, nil
		}
		return PushResult{}, err
	}

	// Second pass mirrors "git push --tags": update every refs/tags/*
	// that exists locally. Like git, the tag refspec is forced, so a
	// remote tag that already points elsewhere is updated.
	if err := pushOnce(repo, remoteCfg, &git.PushOptions{
		RemoteName: remoteName,
		Auth:       auth,
		RefSpecs:   []config.RefSpec{config.RefSpec("+refs/tags/*:refs/tags/*")},
	}); err != nil && !errors.Is(err, git.NoErrAlreadyUpToDate) {
		return PushResult{
			Remote: remoteName,
			Branch: branch,
			Tags:   false,
		}, fmt.Errorf("push tags: %w", err)
	}

	return PushResult{Remote: remoteName, Branch: branch, Tags: true}, nil
}

// pushOnce runs a single push operation through the Remote abstraction.
func pushOnce(repo *git.Repository, cfg *config.RemoteConfig, opts *git.PushOptions) error {
	remote := git.NewRemote(repo.Storer, cfg)
	return remote.PushContext(context.Background(), opts)
}

// getSSHAuth builds an SSH auth method from an SSH private key path,
// listing the remote's configured URLs. Only usable together with an SSH
// remote URL. For non-SSH remotes and empty key paths it returns a nil
// transport.AuthMethod, which makes go-git fall back to its default auth
// (SSH agent for ssh URLs, no auth for https/file URLs).
//
// The server host key is verified against ~/.ssh/known_hosts and
// /etc/ssh/ssh_known_hosts (plus SSH_KNOWN_HOSTS), exactly like git. On top
// of wiring that host-key callback, applyKnownHosts also restricts the
// offered host-key algorithms to the ones actually stored in known_hosts for
// the target host. Without that restriction x/crypto/ssh falls back to its
// full default algorithm list, where ecdsa is preferred over ed25519, so a
// server like github.com ends up presenting an ecdsa key that the
// known_hosts file does not store and the handshake fails with
// "knownhosts: key mismatch" even though "git push" (OpenSSH derives the
// algorithm list from known_hosts itself) succeeds. A host that is not in
// known_hosts at all still fails with a warning rather than silently
// trusting an unknown server.
func getSSHAuth(keyPath string, remoteURLs []string) (transport.AuthMethod, error) {
	last := remoteURLs[len(remoteURLs)-1]
	ep, err := transport.NewEndpoint(last)
	if err != nil {
		return nil, fmt.Errorf("parse remote url: %w", err)
	}
	if ep.Protocol != "ssh" {
		return nil, nil
	}

	user := ep.User
	if user == "" {
		user = os.Getenv("USER")
		if user == "" {
			user = "git"
		}
	}

	hostWithPort := sshHostWithPort(ep)

	var auth transport.AuthMethod
	switch {
	case keyPath == "":
		a, err := sshpkg.NewSSHAgentAuth(user)
		if err != nil {
			return nil, fmt.Errorf("ssh agent auth: %w", err)
		}
		auth = a
	case sign.IsSecurityKeyPath(keyPath):
		// The path names a FIDO2 security-key key (e.g.
		// ~/.ssh/id_ed25519_sk). The private key material lives on the
		// smartcard and can only be used through the ssh-agent, exactly
		// like git's "IdentitiesOnly + PKCS11Provider" story. The file on
		// disk is just a handle to the agent-held key, so the key path
		// selects which agent identity to offer and the signature is
		// delegated to the agent.
		a, err := securityKeyAuth(user, keyPath)
		if err != nil {
			return nil, err
		}
		auth = a
	default:
		a, err := sshpkg.NewPublicKeysFromFile(user, keyPath, "")
		if err != nil {
			return nil, fmt.Errorf("load push key %s: %w", keyPath, err)
		}
		auth = a
	}

	if err := applyKnownHosts(auth, hostWithPort); err != nil {
		// A broken known_hosts setup degrades like every other push
		// problem: warn via the returned error path but keep the auth
		// method so the caller can still attempt the push.
		return auth, err
	}
	return auth, nil
}

// sshHostWithPort builds the "host:port" string used to look up a server in
// the known_hosts files, mirroring go-git's own getHostWithPort. The default
// SSH port is 22 when the URL did not specify one, matching how known_hosts
// entries without an explicit port match port 22.
func sshHostWithPort(ep *transport.Endpoint) string {
	port := ep.Port
	if port <= 0 {
		port = 22
	}
	return net.JoinHostPort(ep.Host, strconv.Itoa(port))
}

// applyKnownHosts wires the known_hosts host-key callback and the matching
// host-key algorithm list into a go-git SSH auth method. It must be called
// for every SSH auth method, because go-git only populates HostKeyAlgorithms
// from known_hosts when the auth method leaves HostKeyCallback nil AND its
// ClientConfig() helper has not already filled that callback, which is the
// case for all auth methods here. Setting both fields ourselves also makes
// connect()'s own known_hosts detection a no-op (it only runs when
// HostKeyCallback is still nil), so the algorithm restriction always applies.
//
// When the host is not present in known_hosts the algorithm list is empty;
// HostKeyAlgorithms is then left unset so x/crypto/ssh uses its defaults and
// the known_hosts callback itself surfaces the usual "key is unknown"
// failure, preserving the "never trust an unknown server" contract.
func applyKnownHosts(auth transport.AuthMethod, hostWithPort string) error {
	db, err := sshpkg.NewKnownHostsDb()
	if err != nil {
		return fmt.Errorf("load known_hosts: %w", err)
	}
	cb := db.HostKeyCallback()
	algos := db.HostKeyAlgorithms(hostWithPort)

	// Both *PublicKeys and *PublicKeysCallback embed HostKeyCallbackHelper,
	// which exposes the exported HostKeyCallback and HostKeyAlgorithms
	// fields. A type switch keeps this forward-compatible if go-git adds
	// more SSH auth method types later.
	switch a := auth.(type) {
	case *sshpkg.PublicKeys:
		a.HostKeyCallback = cb
		if len(algos) > 0 {
			a.HostKeyAlgorithms = algos
		}
	case *sshpkg.PublicKeysCallback:
		a.HostKeyCallback = cb
		if len(algos) > 0 {
			a.HostKeyAlgorithms = algos
		}
	}
	return nil
}

// --- FIDO2 security-key support -------------------------------------------------

// securityKeyAuth builds a go-git SSH auth method that authenticates with a
// FIDO2 security-key identity held by the ssh-agent, instead of a private
// key file. It connects to the agent and offers the identities matching the
// key handle at keyPath (its public half read from disk or the .pub file);
// the agent then talks to the smartcard, which enforces the user-presence
// (touch) check. Requires an ssh-agent; otherwise an error surfaces and the
// caller degrades exactly like for an unusable key file.
func securityKeyAuth(user, keyPath string) (transport.AuthMethod, error) {
	conn, err := net.Dial("unix", os.Getenv("SSH_AUTH_SOCK"))
	if err != nil {
		return nil, fmt.Errorf("connect to ssh-agent for security key %s: %w", keyPath, err)
	}
	ag := agent.NewClient(conn)
	signers, err := ag.Signers()
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("list agent identities for security key %s: %w", keyPath, err)
	}

	matching := make([]ssh.Signer, 0, len(signers))
	for _, s := range signers {
		if sign.HandleMatches(s.PublicKey(), keyPath) {
			matching = append(matching, s)
		}
	}

	cb := func() ([]ssh.Signer, error) {
		if len(matching) == 0 {
			// Key handle selected, but the agent does not hold it.
			// Fall back to offering everything the agent knows, so a
			// user who set OMC_PUSH_KEY_PATH once is not punished
			// when the sk path only selects a name; the agent still
			// picks the right identity per server.
			return signers, nil
		}
		return matching, nil
	}

	auth := &sshpkg.PublicKeysCallback{
		User:     user,
		Callback: cb,
	}
	return auth, nil
}
