// Package gitops implements the git operations behind ocommit: repository
// detection, staging, diffs, committing and history output. All of it is
// done through the go-git library, so the tool has no runtime dependency on
// a git executable.
package gitops

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/filemode"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/storer"
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
	DefaultName  = "OCOMMIT, Git Commiter"
	DefaultEmail = "git@ocommit.local"
)

// Identity is the resolved author/committer for a commit.
type Identity struct {
	Name  string
	Email string
}

// ResolveIdentity returns the commit identity: OCOMMIT_NAME/OCOMMIT_EMAIL
// first, then the standard GIT_AUTHOR_*/GIT_COMMITTER_* variables, then the
// repository's git config, and finally the built-in defaults.
func ResolveIdentity(repo *git.Repository) Identity {
	return identity(repo)
}

// identity reads the identity from the environment and, failing that, from
// the repository's git config, and finally falls back to fixed defaults.
func identity(repo *git.Repository) Identity {
	sig := Identity{Name: DefaultName, Email: DefaultEmail}
	nameEnv := envOr("OCOMMIT_NAME", "GIT_AUTHOR_NAME", "GIT_COMMITTER_NAME")
	emailEnv := envOr("OCOMMIT_EMAIL", "GIT_AUTHOR_EMAIL", "GIT_COMMITTER_EMAIL")
	if nameEnv != "" {
		sig.Name = nameEnv
	}
	if emailEnv != "" {
		sig.Email = emailEnv
	}
	// Fall back to the repo's git config only when no environment variable
	// configured the identity; env always wins.
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

	sig := signatureFrom(identity(repo))
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

	sig := signatureFrom(identity(repo))

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

func Log(repo *git.Repository, limit int) (string, error) {
	if limit <= 0 {
		limit = 5
	}
	cIter, err := repo.Log(&git.LogOptions{Order: git.LogOrderCommitterTime})
	if err != nil {
		return "", fmt.Errorf("open log: %w", err)
	}

	var out bytes.Buffer
	seen := 0
	err = cIter.ForEach(func(c *object.Commit) error {
		if seen >= limit {
			return storer.ErrStop
		}
		seen++
		short := c.Hash.String()
		if len(short) > 7 {
			short = short[:7]
		}
		first, _, _ := strings.Cut(c.Message, "\n")
		fmt.Fprintf(&out, "%s  %s <%s>  %s\n",
			short, c.Author.Name, c.Author.Email, c.Author.When.Format("2006-01-02"))
		fmt.Fprintf(&out, "    %s\n", first)
		if c.PGPSignature != "" {
			out.WriteString("    signed: yes\n")
		}
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("iterate log: %w", err)
	}
	return out.String(), nil
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
				}
			}
			if n == len(parts)-1 {
				node.hash = e.Hash
				node.mode = e.Mode
			}
		}
	}

	var kids []*indexNode
	topLevel := map[string]bool{}
	for _, e := range idx.Entries {
		first, _, _ := strings.Cut(e.Name, "/")
		if !topLevel[first] {
			topLevel[first] = true
			kids = append(kids, nodes[first])
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
