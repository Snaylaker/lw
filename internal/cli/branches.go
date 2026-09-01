package cli

import (
	"context"
	"fmt"
	"strings"

	branchname "github.com/snaylaker/lw/internal/branch"
	"github.com/snaylaker/lw/internal/config"
	"github.com/snaylaker/lw/internal/credential"
	"github.com/snaylaker/lw/internal/domain"
	"github.com/snaylaker/lw/internal/gitrepo"
	"github.com/snaylaker/lw/internal/lwerr"
	"github.com/snaylaker/lw/internal/providers"
	issueprovider "github.com/snaylaker/lw/provider"
)

const (
	branchActionSet     = "set-rule"
	branchActionShow    = "show-rule"
	branchActionPreview = "preview"
	branchActionUnset   = "unset-rule"
)

// runBranches manages the rule for one repository. Repository identity follows
// the worktree flow: normalized origin first, absolute main-checkout path for a
// local-only repository.
func runBranches(ctx context.Context, opts Options, env *execEnv) int {
	action, value, err := parseBranchAction(opts)
	if err != nil {
		return Report(err, env.stderr)
	}
	repo, err := gitrepo.Source(ctx, gitrepo.SourceOptions{
		Flag: config.ExpandTilde(opts.Repo, env.env), Dir: env.dir, Run: env.run,
	})
	if err != nil {
		return Report(err, env.stderr)
	}
	stored, err := config.ReadStoredConfig(env.configPath())
	if err != nil {
		return Report(err, env.stderr)
	}
	preferred, keys := branchRepositoryKeys(ctx, repo, env.run)

	switch action {
	case branchActionSet:
		return setBranchRule(ctx, value, opts.Username, repo, preferred, keys, stored, env)
	case branchActionShow:
		return showBranchRule(preferred, keys, stored, env)
	case branchActionPreview:
		return previewBranchRule(ctx, opts, value, repo, preferred, keys, stored, env)
	case branchActionUnset:
		return unsetBranchRule(preferred, keys, stored, env)
	default:
		return Report(usagef("unknown branches action %s", action), env.stderr)
	}
}

func parseBranchAction(opts Options) (action, value string, err error) {
	if len(opts.Args) == 0 {
		return "", "", usagef("lw branches needs set-rule, show-rule, preview, or unset-rule")
	}
	action = opts.Args[0]
	expected := 1
	if action == branchActionSet || action == branchActionPreview {
		expected = 2
	}
	if len(opts.Args) != expected {
		switch action {
		case branchActionSet:
			return "", "", usagef("lw branches set-rule needs exactly one template")
		case branchActionPreview:
			return "", "", usagef("lw branches preview needs exactly one issue identifier")
		case branchActionShow, branchActionUnset:
			return "", "", usagef("lw branches %s takes no arguments", action)
		default:
			return "", "", usagef("unknown branches action %s", action)
		}
	}
	if opts.Username != "" && action != branchActionSet {
		return "", "", usagef("--username is only valid with lw branches set-rule")
	}
	if opts.Provider != "" && action != branchActionPreview {
		return "", "", usagef("--provider is only valid with lw branches preview")
	}
	if expected == 2 {
		value = opts.Args[1]
	}
	return action, value, nil
}

func branchRepositoryKeys(ctx context.Context, repo domain.Repo, run gitrepo.Runner) (string, []string) {
	remote := branchname.RepositoryKey(ctx, repo, run)
	if remote == "" || remote == repo.Root {
		return repo.Root, []string{repo.Root}
	}
	return remote, []string{remote, repo.Root}
}

func setBranchRule(ctx context.Context, template, suppliedUsername string, repo domain.Repo, preferred string, keys []string, stored *config.StoredConfig, env *execEnv) int {
	_, _, existingUsername, _ := config.BranchRuleEntry(stored, keys...)
	username := strings.TrimSpace(suppliedUsername)
	if username == "" {
		username = existingUsername
	}
	if err := validateBranchRuleTemplate(template, username); err != nil {
		return Report(err, env.stderr)
	}
	// A safe representative issue catches invalid literal separators and an
	// invalid configured username before the rule reaches a real worktree.
	sample := domain.Issue{
		WorktreeKey: "TEAM-123", Title: "branch topic",
		SuggestedBranch: "user/team-123-branch-topic",
	}
	name, err := branchname.Expand(template, sample, username)
	if err != nil {
		return Report(err, env.stderr)
	}
	if err := branchname.ValidateName(ctx, repo, name, env.run); err != nil {
		return Report(err, env.stderr)
	}
	changed, err := config.SetBranchRule(config.BranchRuleUpdate{
		Repository: preferred, Template: template, Username: suppliedUsername,
	}, env.configPath())
	if err != nil {
		return Report(err, env.stderr)
	}
	if changed {
		fmt.Fprintf(env.stdout, "Saved branch rule for %s.\n", preferred)
	} else {
		fmt.Fprintf(env.stdout, "Branch rule for %s is already set.\n", preferred)
	}
	return 0
}

func showBranchRule(preferred string, keys []string, stored *config.StoredConfig, env *execEnv) int {
	repository, template, username, ok := config.BranchRuleEntry(stored, keys...)
	if !ok {
		return Report(missingBranchRule(preferred), env.stderr)
	}
	fmt.Fprintf(env.stdout, "repository: %s\ntemplate: %s\n", repository, template)
	if username != "" {
		fmt.Fprintf(env.stdout, "username: %s\n", username)
	}
	return 0
}

func previewBranchRule(ctx context.Context, opts Options, identifier string, repo domain.Repo, preferred string, keys []string, stored *config.StoredConfig, env *execEnv) int {
	_, template, username, ok := config.BranchRuleEntry(stored, keys...)
	if !ok {
		return Report(missingBranchRule(preferred), env.stderr)
	}
	if err := validateBranchRuleTemplate(template, username); err != nil {
		return Report(err, env.stderr)
	}
	providerOpts := opts
	providerOpts.Issue = identifier
	providerID, err := selectedProvider(providerOpts, stored, env.env, env.registry)
	if err != nil {
		return Report(err, env.stderr)
	}
	if _, reference, ok := prefixedProviderReference(identifier); ok {
		identifier = reference
	}
	var resolved credential.Resolved
	if providerID == issueprovider.Linear {
		resolved, err = credential.Resolve(ctx, credential.Options{
			Env: env.env, Platform: env.platform, Command: storedCredentialCommand(stored),
			ConfigPath: env.configPath(), Run: env.credential, Vault: env.vault,
		})
		if err != nil {
			return Report(err, env.stderr)
		}
	}
	source, err := buildProvider(ctx, providerID, resolved.Credential, &repo, env)
	if err != nil {
		return Report(err, env.stderr)
	}
	if err := source.ValidateReference(identifier); err != nil {
		return Report(usagef("issue is not valid for %s: %s", source.DisplayName(), err), env.stderr)
	}
	item, err := source.Resolve(ctx, identifier)
	if err != nil {
		return Report(err, env.stderr)
	}
	issue, err := providers.Normalize(providerID, item)
	if err != nil {
		return Report(err, env.stderr)
	}
	name, err := branchname.Expand(template, issue, username)
	if err != nil {
		return Report(err, env.stderr)
	}
	if err := branchname.ValidateName(ctx, repo, name, env.run); err != nil {
		return Report(err, env.stderr)
	}
	fmt.Fprintln(env.stdout, name)
	return 0
}

func unsetBranchRule(preferred string, keys []string, stored *config.StoredConfig, env *execEnv) int {
	repository, _, _, ok := config.BranchRuleEntry(stored, keys...)
	if !ok {
		fmt.Fprintf(env.stdout, "No branch rule is configured for %s.\n", preferred)
		return 0
	}
	changed, err := config.UnsetBranchRule(repository, env.configPath())
	if err != nil {
		return Report(err, env.stderr)
	}
	if changed {
		fmt.Fprintf(env.stdout, "Removed branch rule for %s.\n", repository)
	}
	return 0
}

func validateBranchRuleTemplate(template, username string) error {
	if err := branchname.ValidateTemplate(template); err != nil {
		return err
	}
	if strings.Contains(template, "{username}") && strings.TrimSpace(username) == "" {
		return lwerr.New(lwerr.ConfigInvalid,
			"branch template uses {username}, but no username is configured",
			"re-run with --username <gitlab-user-name>")
	}
	return nil
}

func missingBranchRule(repository string) *lwerr.Error {
	return lwerr.New(lwerr.ConfigInvalid,
		"no branch rule is configured for "+repository,
		"run lw branches set-rule <template>")
}
