package branch

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/snaylaker/lw/internal/domain"
	"github.com/snaylaker/lw/internal/gitrepo"
	"github.com/snaylaker/lw/internal/lwerr"
)

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_CONFIG_GLOBAL="+os.DevNull,
		"GIT_CONFIG_SYSTEM="+os.DevNull,
		"GIT_AUTHOR_NAME=lw test", "GIT_AUTHOR_EMAIL=test@lw.invalid",
		"GIT_COMMITTER_NAME=lw test", "GIT_COMMITTER_EMAIL=test@lw.invalid",
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, output)
	}
	return strings.TrimSpace(string(output))
}

func repo(t *testing.T) domain.Repo {
	t.Helper()
	dir := t.TempDir()
	git(t, dir, "init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, dir, "add", "README.md")
	git(t, dir, "commit", "-q", "-m", "initial")
	resolved, err := gitrepo.Resolve(context.Background(), dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	return resolved
}

func ticket() domain.Issue {
	return domain.Issue{Identifier: "ENG-12", Title: "Fix Count Endpoint", SuggestedBranch: "mehdi/eng-12-fix-count-endpoint"}
}

func TestResolveReusesOneBoundaryMatchedLocalBranch(t *testing.T) {
	repository := repo(t)
	git(t, repository.Root, "branch", "mehdi/eng-12-fix")
	git(t, repository.Root, "branch", "mehdi/eng-123-not-this-ticket")

	resolution, err := Resolve(context.Background(), Options{Repo: repository, Issue: ticket()})
	if err != nil {
		t.Fatal(err)
	}
	if resolution.Selected == nil || resolution.Selected.Name != "mehdi/eng-12-fix" || !resolution.Selected.ExistingLocal {
		t.Fatalf("resolution = %+v", resolution)
	}
}

func TestResolveReturnsEveryAmbiguousExistingMatch(t *testing.T) {
	repository := repo(t)
	git(t, repository.Root, "branch", "a/ENG-12-one")
	git(t, repository.Root, "branch", "b/eng-12-two")

	resolution, err := Resolve(context.Background(), Options{Repo: repository, Issue: ticket()})
	if err != nil {
		t.Fatal(err)
	}
	if got := []string{resolution.Candidates[0].Name, resolution.Candidates[1].Name}; strings.Join(got, ",") != "a/ENG-12-one,b/eng-12-two" {
		t.Fatalf("candidates = %v", got)
	}
}

func TestResolveUsesTemplateOnlyWhenNoExistingBranchMatches(t *testing.T) {
	repository := repo(t)
	resolution, err := Resolve(context.Background(), Options{
		Repo: repository, Issue: ticket(), Template: "{username}/{ticket_lower}-{slug}", Username: "mehdi",
	})
	if err != nil {
		t.Fatal(err)
	}
	if resolution.Selected == nil || resolution.Selected.Name != "mehdi/eng-12-fix-count-endpoint" || resolution.Selected.Base != "HEAD" {
		t.Fatalf("resolution = %+v", resolution)
	}
}

func TestResolveLeavesLinearSuggestionEditableWithoutACreationRule(t *testing.T) {
	resolution, err := Resolve(context.Background(), Options{Repo: repo(t), Issue: ticket()})
	if err != nil {
		t.Fatal(err)
	}
	if resolution.Selected != nil || resolution.Suggested != ticket().SuggestedBranch {
		t.Fatalf("resolution = %+v", resolution)
	}
}

func TestExplicitBranchOverridesExistingTicketMatches(t *testing.T) {
	repository := repo(t)
	git(t, repository.Root, "branch", "old/eng-12")
	resolution, err := Resolve(context.Background(), Options{Repo: repository, Issue: ticket(), Explicit: "chosen/topic"})
	if err != nil {
		t.Fatal(err)
	}
	if resolution.Selected == nil || resolution.Selected.Name != "chosen/topic" {
		t.Fatalf("resolution = %+v", resolution)
	}
}

func TestInvalidExplicitBranchIsActionable(t *testing.T) {
	_, err := Resolve(context.Background(), Options{Repo: repo(t), Issue: ticket(), Explicit: "bad branch"})
	if !lwerr.Is(err, lwerr.WorktreeConflict) {
		t.Fatalf("error = %v", err)
	}
}

func TestValidateTemplateDoesNotNeedAnIssue(t *testing.T) {
	for _, template := range []string{
		"{username}/{ticket}/{slug}",
		"{linear_branch}",
		"release/topic",
	} {
		if err := ValidateTemplate(template); err != nil {
			t.Errorf("ValidateTemplate(%q): %v", template, err)
		}
	}
	for _, template := range []string{"", "{owner}/{ticket}", "{Ticket}"} {
		if err := ValidateTemplate(template); !lwerr.Is(err, lwerr.ConfigInvalid) {
			t.Errorf("ValidateTemplate(%q) error = %v", template, err)
		}
	}
}

func TestMatchingBranchesUsesProviderBranchKeys(t *testing.T) {
	found := refs{
		local:  map[string]bool{"alex/42-repair-cache": true, "alex/unrelated": true},
		remote: map[string]string{},
	}
	matches := matchingBranches(found, []string{"GH-acme-api-42", "42"})
	if len(matches) != 1 || matches[0].Name != "alex/42-repair-cache" {
		t.Fatalf("matches = %+v", matches)
	}
}

func TestExpandRejectsUnknownAndMissingTemplateVariables(t *testing.T) {
	if _, err := Expand("{owner}/{ticket}", ticket(), "mehdi"); !lwerr.Is(err, lwerr.ConfigInvalid) {
		t.Fatalf("unknown placeholder error = %v", err)
	}
	if _, err := Expand("{username}/{ticket}", ticket(), ""); !lwerr.Is(err, lwerr.ConfigInvalid) {
		t.Fatalf("missing username error = %v", err)
	}
}

func TestResolveFetchesOriginAndBasesNewBranchesOnItsDefault(t *testing.T) {
	bare := t.TempDir()
	git(t, bare, "init", "-q", "--bare", "--initial-branch=main")
	seed := t.TempDir()
	git(t, seed, "init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(seed, "version.txt"), []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, seed, "add", "version.txt")
	git(t, seed, "commit", "-q", "-m", "one")
	git(t, seed, "remote", "add", "origin", bare)
	git(t, seed, "push", "-q", "-u", "origin", "main")

	checkout := filepath.Join(t.TempDir(), "checkout")
	git(t, filepath.Dir(checkout), "clone", "-q", bare, checkout)
	if err := os.WriteFile(filepath.Join(seed, "version.txt"), []byte("two\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, seed, "commit", "-q", "-am", "two")
	git(t, seed, "push", "-q", "origin", "main")
	git(t, seed, "branch", "alex/eng-13-remote")
	git(t, seed, "push", "-q", "origin", "alex/eng-13-remote")
	repository, err := gitrepo.Resolve(context.Background(), checkout, nil)
	if err != nil {
		t.Fatal(err)
	}

	resolution, err := Resolve(context.Background(), Options{
		Repo: repository, Issue: ticket(), Template: "{ticket_lower}-{slug}",
	})
	if err != nil {
		t.Fatal(err)
	}
	if resolution.Selected == nil || resolution.Selected.Base != "origin/main" {
		t.Fatalf("resolution = %+v", resolution)
	}
	if got := git(t, checkout, "show", "origin/main:version.txt"); got != "two" {
		t.Fatalf("origin/main was not refreshed: %q", got)
	}

	remoteIssue := domain.Issue{Identifier: "ENG-13", Title: "Remote work"}
	remoteResolution, err := Resolve(context.Background(), Options{Repo: repository, Issue: remoteIssue})
	if err != nil {
		t.Fatal(err)
	}
	if remoteResolution.Selected == nil || remoteResolution.Selected.ExistingRemote != "refs/remotes/origin/alex/eng-13-remote" {
		t.Fatalf("remote resolution = %+v", remoteResolution)
	}
}

func TestRepositoryKeyNormalizesCommonOriginURLs(t *testing.T) {
	repository := repo(t)
	git(t, repository.Root, "remote", "add", "origin", "git@gitlab.example.com:group/api.git")
	if got := RepositoryKey(context.Background(), repository, nil); got != "gitlab.example.com/group/api" {
		t.Fatalf("key = %q", got)
	}
}
