package worktree

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/snaylaker/lw/internal/config"
	"github.com/snaylaker/lw/internal/domain"
	"github.com/snaylaker/lw/internal/gitrepo"
	"github.com/snaylaker/lw/internal/lwerr"
)

// MetadataFileName lives in the worktree's git directory rather than in the
// checkout: it must never show up as an untracked file, and git already keeps
// one private directory per worktree.
const MetadataFileName = "lw.json"

// Metadata is what a worktree knows about itself, and the whole integration
// surface of this tool. Other programs are expected to read it, so the field
// names are a published contract: add keys, never rename or remove them.
//
// The file keeps every key present. A reader of `summary` gets "" on a fresh
// worktree, never undefined. Branch is stored because it is no longer implied
// by Identifier.
type Metadata struct {
	Identifier string `json:"identifier"`
	Provider   string `json:"provider"`
	ExternalID string `json:"externalId"`
	Reference  string `json:"reference"`
	Title      string `json:"title"`
	URL        string `json:"url"`
	Team       string `json:"team"`
	Branch     string `json:"branch"`
	// Summary is written by nobody at creation time; it is the one field a later
	// run, or another program, is expected to update.
	Summary string `json:"summary"`
}

// MetadataOf is the metadata a freshly created worktree carries.
func MetadataOf(issue domain.Issue, selectedBranch ...string) Metadata {
	branch := issue.Identifier
	if len(selectedBranch) > 0 && selectedBranch[0] != "" {
		branch = selectedBranch[0]
	}
	return Metadata{
		Identifier: issue.Identifier,
		Provider:   issue.Provider,
		ExternalID: issue.ExternalID,
		Reference:  issue.DisplayReference(),
		Title:      issue.Title,
		URL:        issue.URL,
		Team:       issue.TeamKey,
		Branch:     branch,
	}
}

// GitDir is the absolute git directory of the worktree dir sits in:
// <repo>/.git/worktrees/<name> for a linked worktree, <repo>/.git for the main
// checkout. This is the lookup that makes the metadata findable from any depth
// inside a checkout without an environment variable or a daemon.
func GitDir(ctx context.Context, dir string, run gitrepo.Runner) (string, error) {
	if run == nil {
		run = gitrepo.DefaultRunner
	}
	result, err := run(ctx, dir, "git", []string{"rev-parse", "--absolute-git-dir"})
	if gitrepo.IsGitMissing(err) {
		// SPEC §4: a missing git is its own failure. "cd into a repository" is
		// a dead end when nothing can run git anywhere.
		return "", lwerr.Wrap(err, lwerr.NotARepo,
			gitrepo.GitMissingMessage,
			gitrepo.GitMissingNextAction,
		)
	}
	if err != nil || result.ExitCode != 0 || strings.TrimSpace(result.Stdout) == "" {
		// The same failure as SPEC §4's, in the same words: this is the other
		// door into "you are not standing in a repository".
		return "", lwerr.Wrap(err, lwerr.NotARepo,
			gitrepo.NotARepoMessage,
			gitrepo.NotARepoNextAction,
		)
	}
	return strings.TrimSpace(result.Stdout), nil
}

// MetadataPath is where the metadata of the worktree owning gitDir lives.
func MetadataPath(gitDir string) string { return filepath.Join(gitDir, MetadataFileName) }

// PruneOrphanedMetadata removes the metadata of a worktree whose branch no
// longer exists, and reports whether it removed anything.
//
// The case it catches: someone switches the worktree to another branch and
// deletes the issue branch, leaving lw.json claiming a ticket that has no
// branch. `lw context` would then tell an agent it is working ENG-3971 when
// nothing of the sort is checked out.
//
// It runs on every command, so it is deliberately one rev-parse and strictly
// best effort: any uncertainty — git missing, not a repository, an unreadable
// file, a git that could not answer — leaves the file exactly where it is.
// Deleting metadata is not worth a guess.
func PruneOrphanedMetadata(ctx context.Context, dir string, run gitrepo.Runner) (bool, error) {
	if run == nil {
		run = gitrepo.DefaultRunner
	}
	gitDir, err := GitDir(ctx, dir, run)
	if err != nil {
		return false, nil
	}
	path := MetadataPath(gitDir)
	metadata, err := readMetadataFile(path)
	if err != nil || metadata == nil || metadata.Identifier == "" {
		return false, nil
	}

	branch := metadata.Branch
	if branch == "" { // metadata written before branch names became configurable
		branch = metadata.Identifier
	}
	exists, ok := branchExists(ctx, dir, branch, run)
	if !ok || exists {
		return false, nil
	}

	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return false, err
	}
	return true, nil
}

// ReadMetadata finds the metadata from any directory inside a worktree. It
// returns nil for "there is nothing here to read" — no metadata file, or not a
// git repository at all — so a reader that only wants context when it exists
// needs no error handling. A file that exists but cannot be used is an error,
// because reading it as absent would hide a real problem.
func ReadMetadata(ctx context.Context, dir string, run gitrepo.Runner) (*Metadata, error) {
	gitDir, err := GitDir(ctx, dir, run)
	if err != nil {
		return nil, nil
	}
	return readMetadataFile(MetadataPath(gitDir))
}

// WriteMetadata replaces the metadata of the worktree at dir. Writing is atomic
// so a reader in another process never sees a half-written file.
func WriteMetadata(ctx context.Context, dir string, metadata Metadata, run gitrepo.Runner) error {
	gitDir, err := GitDir(ctx, dir, run)
	if err != nil {
		return err
	}
	return writeMetadataFile(MetadataPath(gitDir), metadata)
}

// UpdateSummary sets the one mutable field, keeping the rest as written at
// creation, and returns the stored result. A worktree with no metadata yet
// cannot be summarised: the identifier would be a guess.
func UpdateSummary(ctx context.Context, dir, summary string, run gitrepo.Runner) (*Metadata, error) {
	gitDir, err := GitDir(ctx, dir, run)
	if err != nil {
		return nil, err
	}
	path := MetadataPath(gitDir)
	metadata, err := readMetadataFile(path)
	if err != nil {
		return nil, err
	}
	if metadata == nil {
		return nil, lwerr.New(lwerr.Internal,
			`"`+dir+`" carries no worktree metadata`,
			"run lw from the repository to create the worktree first",
		)
	}
	metadata.Summary = strings.TrimSpace(summary)
	if err := writeMetadataFile(path, *metadata); err != nil {
		return nil, err
	}
	return metadata, nil
}

func readMetadataFile(path string) (*Metadata, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ENOTDIR) {
			return nil, nil
		}
		return nil, metadataInvalid(err, path, "cannot be read: "+err.Error())
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return nil, nil
	}
	var metadata Metadata
	if err := json.Unmarshal(data, &metadata); err != nil {
		return nil, metadataInvalid(err, path, "is not valid JSON: "+err.Error())
	}
	if metadata.Identifier == "" {
		return nil, metadataInvalid(nil, path, "has no identifier")
	}
	return &metadata, nil
}

func writeMetadataFile(path string, metadata Metadata) error {
	payload, err := config.MarshalIndented(metadata)
	if err != nil {
		return lwerr.Wrap(err, lwerr.Internal,
			"the worktree metadata could not be encoded",
			"report this: it is a bug in lw",
		)
	}
	if err := config.AtomicWriteJSON(path, payload); err != nil {
		return lwerr.Wrap(err, lwerr.Internal,
			"the worktree metadata could not be written to "+path,
			"check that the repository's git directory is writable",
		)
	}
	return nil
}

func metadataInvalid(cause error, path, problem string) *lwerr.Error {
	return lwerr.Wrap(cause, lwerr.Internal,
		"the worktree metadata at "+path+" "+problem,
		"delete the file and re-open the issue with lw",
	)
}
