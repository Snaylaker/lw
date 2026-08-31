package worktree

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/snaylaker/lw/internal/gitrepo"
)

func TestMetadataIsFoundFromAnywhereInsideTheWorktree(t *testing.T) {
	repo := newRepo(t)
	root := t.TempDir()
	result, err := open(t, repo, root, "ENG-3971", nil)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	nested := filepath.Join(result.Path, "src", "billing")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}

	metadata, err := ReadMetadata(context.Background(), nested, nil)
	if err != nil || metadata == nil {
		t.Fatalf("ReadMetadata = %v, %v", metadata, err)
	}
	if metadata.Identifier != "ENG-3971" {
		t.Fatalf("metadata = %+v", *metadata)
	}
}

// The file is the product's whole integration surface, so its keys are read
// here the way another program would read them.
func TestMetadataIsPlainDocumentedJSON(t *testing.T) {
	repo := newRepo(t)
	result, err := open(t, repo, t.TempDir(), "ENG-3971", nil)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	gitDir, err := GitDir(context.Background(), result.Path, nil)
	if err != nil {
		t.Fatalf("GitDir: %v", err)
	}
	path := MetadataPath(gitDir)
	if filepath.Base(path) != "lw.json" || !filepath.IsAbs(path) {
		t.Fatalf("metadata path = %q", path)
	}
	if inside := filepath.Join(result.Path, "lw.json"); path == inside {
		t.Fatal("the metadata must not sit in the checkout, where it would show up as untracked")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err := json.Unmarshal(data, &fields); err != nil {
		t.Fatalf("unmarshalling %s: %v", data, err)
	}
	// The spec gives the file's content literally, all keys present. This file
	// is the product's entire integration surface: a consumer reading
	// `summary` on a fresh worktree must get "", never undefined.
	want := map[string]any{
		"identifier": "ENG-3971",
		"title":      "Something to do in ENG-3971",
		"url":        "https://linear.app/acme/issue/ENG-3971",
		"team":       "ENG",
		"branch":     "ENG-3971",
		"summary":    "",
	}
	if len(fields) != len(want) {
		t.Fatalf("lw.json = %v, want exactly the documented fields", fields)
	}
	for key, value := range want {
		if fields[key] != value {
			t.Fatalf("%s = %v, want %v", key, fields[key], value)
		}
	}
}

func TestUpdateSummaryKeepsEverythingElse(t *testing.T) {
	repo := newRepo(t)
	result, err := open(t, repo, t.TempDir(), "ENG-3971", nil)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	updated, err := UpdateSummary(context.Background(), result.Path, "  reproduced the regression  ", nil)
	if err != nil {
		t.Fatalf("UpdateSummary: %v", err)
	}
	if updated.Summary != "reproduced the regression" {
		t.Fatalf("summary = %q", updated.Summary)
	}

	reread, err := ReadMetadata(context.Background(), result.Path, nil)
	if err != nil || reread == nil {
		t.Fatalf("ReadMetadata = %v, %v", reread, err)
	}
	if reread.Summary != "reproduced the regression" || reread.Identifier != "ENG-3971" ||
		reread.Title != "Something to do in ENG-3971" {
		t.Fatalf("metadata = %+v", *reread)
	}
}

func TestReadMetadataIsAbsentRatherThanFailing(t *testing.T) {
	repo := newRepo(t)

	// A repository nobody opened through lw.
	metadata, err := ReadMetadata(context.Background(), repo.Root, nil)
	if metadata != nil || err != nil {
		t.Fatalf("ReadMetadata in a plain repository = %v, %v", metadata, err)
	}

	// A directory that is not a repository at all.
	metadata, err = ReadMetadata(context.Background(), t.TempDir(), nil)
	if metadata != nil || err != nil {
		t.Fatalf("ReadMetadata outside a repository = %v, %v", metadata, err)
	}
}

func TestUnusableMetadataIsReportedRatherThanIgnored(t *testing.T) {
	repo := newRepo(t)
	result, err := open(t, repo, t.TempDir(), "ENG-1", nil)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	gitDir, err := GitDir(context.Background(), result.Path, nil)
	if err != nil {
		t.Fatal(err)
	}
	write(t, MetadataPath(gitDir), "{not json")

	if _, err := ReadMetadata(context.Background(), result.Path, nil); err == nil {
		t.Fatal("a corrupt metadata file must not read as absent")
	}
	if _, err := UpdateSummary(context.Background(), result.Path, "x", nil); err == nil {
		t.Fatal("a corrupt metadata file must not be silently rewritten")
	}
}

func TestUpdateSummaryNeedsMetadataToUpdate(t *testing.T) {
	repo := newRepo(t)
	if _, err := UpdateSummary(context.Background(), repo.Root, "x", nil); err == nil {
		t.Fatal("a repository lw never opened has no summary to set")
	}
	if _, err := UpdateSummary(context.Background(), t.TempDir(), "x", nil); err == nil {
		t.Fatal("a directory outside a repository has no summary to set")
	}
}

// Metadata naming a branch that no longer exists is reaped on the next
// command. The realistic route there is switching the worktree off the issue
// branch and deleting it — git refuses to delete a branch a worktree holds.
func TestOrphanedMetadataIsRemovedWhenTheBranchIsGone(t *testing.T) {
	repo := newRepo(t)
	result, err := open(t, repo, t.TempDir(), "ENG-3971", nil)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	gitDir, err := GitDir(context.Background(), result.Path, nil)
	if err != nil {
		t.Fatalf("GitDir: %v", err)
	}
	path := MetadataPath(gitDir)
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("metadata should exist before the branch goes: %v", err)
	}

	// Let go of the branch, then delete it, exactly as a user would.
	git(t, result.Path, "switch", "-q", "--detach")
	git(t, repo.Root, "branch", "-q", "-D", "ENG-3971")

	removed, err := PruneOrphanedMetadata(context.Background(), result.Path, nil)
	if err != nil {
		t.Fatalf("PruneOrphanedMetadata: %v", err)
	}
	if !removed {
		t.Error("removed = false, want true for a branch that is gone")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("metadata still present after the branch was deleted (err = %v)", err)
	}

	metadata, err := ReadMetadata(context.Background(), result.Path, nil)
	if err != nil || metadata != nil {
		t.Errorf("ReadMetadata = %v, %v; want nil, nil so lw context stays silent", metadata, err)
	}
}

// The other half, and the one that matters more: a live branch must never lose
// its metadata. This is the assertion that fails if the check is inverted.
func TestMetadataSurvivesWhileTheBranchExists(t *testing.T) {
	repo := newRepo(t)
	result, err := open(t, repo, t.TempDir(), "ENG-3971", nil)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	for i := 0; i < 3; i++ {
		removed, err := PruneOrphanedMetadata(context.Background(), result.Path, nil)
		if err != nil {
			t.Fatalf("PruneOrphanedMetadata: %v", err)
		}
		if removed {
			t.Fatalf("run %d removed the metadata of a branch that still exists", i)
		}
	}

	metadata, err := ReadMetadata(context.Background(), result.Path, nil)
	if err != nil {
		t.Fatalf("ReadMetadata: %v", err)
	}
	if metadata == nil || metadata.Identifier != "ENG-3971" {
		t.Fatalf("metadata = %v, want ENG-3971 intact", metadata)
	}
}

// Uncertainty must never delete: a git that cannot answer leaves the file.
func TestPruneLeavesMetadataAloneWhenGitCannotAnswer(t *testing.T) {
	repo := newRepo(t)
	result, err := open(t, repo, t.TempDir(), "ENG-3971", nil)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	gitDir, err := GitDir(context.Background(), result.Path, nil)
	if err != nil {
		t.Fatalf("GitDir: %v", err)
	}

	failing := func(ctx context.Context, dir, name string, args []string) (gitrepo.ExecResult, error) {
		if len(args) > 0 && args[0] == "rev-parse" && len(args) > 1 && args[1] == "--verify" {
			return gitrepo.ExecResult{}, errors.New("git exploded")
		}
		return gitrepo.DefaultRunner(ctx, dir, name, args)
	}

	removed, err := PruneOrphanedMetadata(context.Background(), result.Path, failing)
	if err != nil {
		t.Fatalf("PruneOrphanedMetadata: %v", err)
	}
	if removed {
		t.Error("removed = true; a git that could not answer must not cost the user their metadata")
	}
	if _, err := os.Stat(MetadataPath(gitDir)); err != nil {
		t.Errorf("metadata was deleted on an unanswerable git: %v", err)
	}
}
