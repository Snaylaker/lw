// Package branch resolves the git branch for a Linear issue before a worktree
// is created. It inspects refs and expands safe templates; it never executes a
// configured command.
package branch

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"unicode"

	"github.com/snaylaker/lw/internal/domain"
	"github.com/snaylaker/lw/internal/gitrepo"
	"github.com/snaylaker/lw/internal/lwerr"
)

type Options struct {
	Repo     domain.Repo
	Issue    domain.Issue
	Explicit string
	Template string
	Username string
	Run      gitrepo.Runner
}

// Resolve refreshes origin when present, inspects local and remote-tracking
// refs, then applies the resolution order: explicit name, existing ticket
// branch, configured template, editable Linear suggestion.
func Resolve(ctx context.Context, options Options) (domain.BranchResolution, error) {
	run := options.Run
	if run == nil {
		run = gitrepo.DefaultRunner
	}
	if err := refreshOrigin(ctx, options.Repo, run); err != nil {
		return domain.BranchResolution{}, err
	}
	refs, err := listRefs(ctx, options.Repo, run)
	if err != nil {
		return domain.BranchResolution{}, err
	}

	if name := strings.TrimSpace(options.Explicit); name != "" {
		selected, err := plan(ctx, options.Repo, name, refs, run)
		if err != nil {
			return domain.BranchResolution{}, err
		}
		return selectedResolution(selected), nil
	}

	matches := matchingBranches(refs, options.Issue.Identifier)
	switch len(matches) {
	case 1:
		return selectedResolution(matches[0]), nil
	case 0:
		// Continue to the repository's creation rule.
	default:
		return domain.BranchResolution{Candidates: matches}, nil
	}

	if strings.TrimSpace(options.Template) != "" {
		name, err := Expand(options.Template, options.Issue, options.Username)
		if err != nil {
			return domain.BranchResolution{}, err
		}
		selected, err := plan(ctx, options.Repo, name, refs, run)
		if err != nil {
			return domain.BranchResolution{}, err
		}
		return selectedResolution(selected), nil
	}

	suggested := strings.TrimSpace(options.Issue.SuggestedBranch)
	if suggested == "" {
		suggested = strings.ToLower(options.Issue.Identifier) + "-" + slug(options.Issue.Title)
		suggested = strings.TrimSuffix(suggested, "-")
	}
	return domain.BranchResolution{Suggested: suggested}, nil
}

// Choose validates and plans an editable branch name after the interactive
// prompt. Resolve has already fetched, so this path only re-reads refs.
func Choose(ctx context.Context, repo domain.Repo, name string, run gitrepo.Runner) (domain.Branch, error) {
	if run == nil {
		run = gitrepo.DefaultRunner
	}
	refs, err := listRefs(ctx, repo, run)
	if err != nil {
		return domain.Branch{}, err
	}
	return plan(ctx, repo, strings.TrimSpace(name), refs, run)
}

func selectedResolution(branch domain.Branch) domain.BranchResolution {
	selected := branch
	return domain.BranchResolution{Selected: &selected}
}

type refs struct {
	local  map[string]bool
	remote map[string]string // local branch name -> remote-tracking ref
}

func refreshOrigin(ctx context.Context, repo domain.Repo, run gitrepo.Runner) error {
	remote, err := run(ctx, repo.Root, "git", []string{"remote", "get-url", "origin"})
	if err != nil {
		if ctx.Err() != nil {
			return lwerr.NewCancelled()
		}
		return gitFailure("could not inspect git remotes", repo.Root)
	}
	if remote.ExitCode != 0 || strings.TrimSpace(remote.Stdout) == "" {
		return nil // A local-only repository has no remote to refresh.
	}
	result, err := run(ctx, repo.Root, "git", []string{"fetch", "--prune", "--quiet", "origin"})
	if err != nil || result.ExitCode != 0 {
		if ctx.Err() != nil {
			return lwerr.NewCancelled()
		}
		return lwerr.Wrap(err, lwerr.Internal,
			"could not fetch origin for "+repo.Name,
			"check your network and remote access, then re-run",
		)
	}
	return nil
}

func listRefs(ctx context.Context, repo domain.Repo, run gitrepo.Runner) (refs, error) {
	result, err := run(ctx, repo.Root, "git", []string{
		"for-each-ref", "--format=%(refname)", "refs/heads", "refs/remotes/origin",
	})
	if err != nil || result.ExitCode != 0 {
		if ctx.Err() != nil {
			return refs{}, lwerr.NewCancelled()
		}
		return refs{}, gitFailure("could not inspect branches", repo.Root)
	}
	found := refs{local: map[string]bool{}, remote: map[string]string{}}
	for _, line := range strings.Split(result.Stdout, "\n") {
		ref := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(ref, "refs/heads/"):
			found.local[strings.TrimPrefix(ref, "refs/heads/")] = true
		case strings.HasPrefix(ref, "refs/remotes/origin/") && ref != "refs/remotes/origin/HEAD":
			name := strings.TrimPrefix(ref, "refs/remotes/origin/")
			found.remote[name] = ref
		}
	}
	return found, nil
}

func matchingBranches(found refs, identifier string) []domain.Branch {
	names := map[string]bool{}
	for name := range found.local {
		if containsIdentifier(name, identifier) {
			names[name] = true
		}
	}
	for name := range found.remote {
		if containsIdentifier(name, identifier) {
			names[name] = true
		}
	}
	ordered := make([]string, 0, len(names))
	for name := range names {
		ordered = append(ordered, name)
	}
	sort.Strings(ordered)
	result := make([]domain.Branch, 0, len(ordered))
	for _, name := range ordered {
		result = append(result, branchFromRefs(name, found))
	}
	return result
}

func containsIdentifier(branchName, identifier string) bool {
	name := strings.ToLower(branchName)
	ticket := strings.ToLower(identifier)
	for offset := 0; offset <= len(name)-len(ticket); {
		index := strings.Index(name[offset:], ticket)
		if index < 0 {
			return false
		}
		index += offset
		beforeOK := index == 0 || !asciiAlphaNumeric(name[index-1])
		after := index + len(ticket)
		afterOK := after == len(name) || !asciiAlphaNumeric(name[after])
		if beforeOK && afterOK {
			return true
		}
		offset = index + 1
	}
	return false
}

func asciiAlphaNumeric(value byte) bool {
	return value >= 'a' && value <= 'z' || value >= '0' && value <= '9'
}

func plan(ctx context.Context, repo domain.Repo, name string, found refs, run gitrepo.Runner) (domain.Branch, error) {
	if err := ValidateName(ctx, repo, name, run); err != nil {
		return domain.Branch{}, err
	}
	selected := branchFromRefs(name, found)
	if !selected.ExistingLocal && selected.ExistingRemote == "" {
		selected.Base = defaultBase(ctx, repo, found, run)
	}
	return selected, nil
}

func branchFromRefs(name string, found refs) domain.Branch {
	selected := domain.Branch{Name: name, ExistingLocal: found.local[name]}
	if !selected.ExistingLocal {
		selected.ExistingRemote = found.remote[name]
	}
	return selected
}

func defaultBase(ctx context.Context, repo domain.Repo, found refs, run gitrepo.Runner) string {
	result, err := run(ctx, repo.Root, "git", []string{"symbolic-ref", "--short", "refs/remotes/origin/HEAD"})
	if err == nil && result.ExitCode == 0 {
		if name := strings.TrimSpace(result.Stdout); name != "" {
			return name
		}
	}
	for _, name := range []string{"main", "master"} {
		if remote := found.remote[name]; remote != "" {
			return remote
		}
	}
	return "HEAD"
}

var placeholderRE = regexp.MustCompile(`\{[a-z_]+\}`)

var knownPlaceholders = map[string]bool{
	"{username}": true, "{ticket}": true, "{ticket_lower}": true,
	"{slug}": true, "{linear_branch}": true,
}

// ValidateTemplate checks the template language without needing a Linear
// issue. It validates syntax and placeholder names, not values or the final Git
// ref; preview and worktree creation perform those later checks.
func ValidateTemplate(template string) error {
	template = strings.TrimSpace(template)
	if template == "" {
		return lwerr.New(lwerr.ConfigInvalid, "branch template is empty",
			"provide a template such as {username}/{ticket}/{slug}")
	}
	for _, placeholder := range placeholderRE.FindAllString(template, -1) {
		if !knownPlaceholders[placeholder] {
			return lwerr.New(lwerr.ConfigInvalid,
				"branch template uses unknown placeholder "+placeholder,
				"use {username}, {ticket}, {ticket_lower}, {slug}, or {linear_branch}")
		}
	}
	if strings.ContainsAny(placeholderRE.ReplaceAllString(template, ""), "{}") {
		return lwerr.New(lwerr.ConfigInvalid,
			"branch template contains an invalid placeholder",
			"use {username}, {ticket}, {ticket_lower}, {slug}, or {linear_branch}")
	}
	return nil
}

// Expand applies the documented placeholders and rejects missing values. This
// is deliberately not a shell expansion language.
func Expand(template string, issue domain.Issue, username string) (string, error) {
	if err := ValidateTemplate(template); err != nil {
		return "", err
	}
	values := map[string]string{
		"{username}":      strings.TrimSpace(username),
		"{ticket}":        issue.Identifier,
		"{ticket_lower}":  strings.ToLower(issue.Identifier),
		"{slug}":          slug(issue.Title),
		"{linear_branch}": strings.TrimSpace(issue.SuggestedBranch),
	}
	for _, placeholder := range placeholderRE.FindAllString(template, -1) {
		value := values[placeholder]
		if value == "" {
			return "", lwerr.New(lwerr.ConfigInvalid,
				"branch template cannot expand "+placeholder,
				"set its value in branchNaming.variables or choose another placeholder")
		}
		template = strings.ReplaceAll(template, placeholder, value)
	}
	return strings.TrimSpace(template), nil
}

// ValidateName asks Git itself whether an expanded or explicitly entered name
// is a valid branch ref.
func ValidateName(ctx context.Context, repo domain.Repo, name string, run gitrepo.Runner) error {
	if run == nil {
		run = gitrepo.DefaultRunner
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return invalidName(name, "branch name is empty")
	}
	checked, err := run(ctx, repo.Root, "git", []string{"check-ref-format", "--branch", name})
	if err != nil || checked.ExitCode != 0 {
		if ctx.Err() != nil {
			return lwerr.NewCancelled()
		}
		reason := firstLine(checked.Stderr)
		if reason == "git gave no reason" {
			reason = "git rejected the name"
		}
		return invalidName(name, reason)
	}
	return nil
}

func slug(title string) string {
	var result []rune
	separator := false
	for _, r := range strings.ToLower(strings.TrimSpace(title)) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			if separator && len(result) > 0 {
				result = append(result, '-')
			}
			separator = false
			result = append(result, r)
		} else {
			separator = true
		}
		if len(result) >= 60 {
			break
		}
	}
	return strings.Trim(string(result), "-")
}

func invalidName(name, reason string) *lwerr.Error {
	return lwerr.New(lwerr.WorktreeConflict,
		fmt.Sprintf("%q is not a valid branch name: %s", name, reason),
		"edit the branch name and try again")
}

func gitFailure(action, root string) *lwerr.Error {
	return lwerr.New(lwerr.Internal, action+" in "+root,
		"check that git works in this repository, then re-run")
}

func firstLine(text string) string {
	text = strings.TrimSpace(text)
	if index := strings.IndexByte(text, '\n'); index >= 0 {
		text = strings.TrimSpace(text[:index])
	}
	if text == "" {
		return "git gave no reason"
	}
	return text
}

// RepositoryKey normalizes common HTTPS, SSH URL, and scp-like origin forms to
// host/path without a trailing .git. Empty means there is no usable origin.
func RepositoryKey(ctx context.Context, repo domain.Repo, run gitrepo.Runner) string {
	if run == nil {
		run = gitrepo.DefaultRunner
	}
	result, err := run(ctx, repo.Root, "git", []string{"remote", "get-url", "origin"})
	if err != nil || result.ExitCode != 0 {
		return ""
	}
	value := strings.TrimSpace(result.Stdout)
	if parsed, err := url.Parse(value); err == nil && parsed.Host != "" {
		return cleanRepositoryKey(parsed.Host + "/" + strings.TrimPrefix(parsed.Path, "/"))
	}
	if at := strings.LastIndex(value, "@"); at >= 0 {
		value = value[at+1:]
	}
	if colon := strings.IndexByte(value, ':'); colon >= 0 {
		return cleanRepositoryKey(value[:colon] + "/" + value[colon+1:])
	}
	return ""
}

func cleanRepositoryKey(value string) string {
	return strings.TrimSuffix(strings.Trim(strings.TrimSpace(value), "/"), ".git")
}
