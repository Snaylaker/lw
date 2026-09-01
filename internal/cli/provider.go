package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/snaylaker/lw/internal/branch"
	"github.com/snaylaker/lw/internal/config"
	"github.com/snaylaker/lw/internal/domain"
	githubprovider "github.com/snaylaker/lw/internal/providers/github"
	jiraprovider "github.com/snaylaker/lw/internal/providers/jira"
	linearprovider "github.com/snaylaker/lw/internal/providers/linear"
	issueprovider "github.com/snaylaker/lw/provider"
)

const providerEnvVar = "LW_ISSUE_PROVIDER"

func prefixedProviderReference(value string) (issueprovider.ID, string, bool) {
	prefix, reference, found := strings.Cut(strings.TrimSpace(value), ":")
	if !found || reference == "" {
		return "", value, false
	}
	switch strings.ToLower(prefix) {
	case "linear":
		return issueprovider.Linear, reference, true
	case "github":
		return issueprovider.GitHub, reference, true
	case "jira":
		return issueprovider.Jira, reference, true
	default:
		return "", value, false
	}
}

func selectedProvider(opts Options, stored *config.StoredConfig, env map[string]string, extensions map[issueprovider.ID]issueprovider.Provider) (issueprovider.ID, error) {
	value := strings.TrimSpace(opts.Provider)
	if prefix, _, ok := prefixedProviderReference(opts.Issue); ok {
		if value != "" && !strings.EqualFold(value, string(prefix)) {
			return "", usagef("--provider %s conflicts with the %s: issue reference", value, prefix)
		}
		value = string(prefix)
	}
	if value == "" {
		value = strings.TrimSpace(env[providerEnvVar])
	}
	if value == "" && stored != nil {
		value = stored.IssueProvider
	}
	if value == "" {
		return issueprovider.Linear, nil
	}
	switch strings.ToLower(value) {
	case string(issueprovider.Linear):
		return issueprovider.Linear, nil
	case string(issueprovider.GitHub):
		return issueprovider.GitHub, nil
	case string(issueprovider.Jira):
		return issueprovider.Jira, nil
	default:
		id := issueprovider.ID(strings.ToLower(value))
		if extensions[id] != nil {
			return id, nil
		}
		return "", usagef("unknown issue provider %q; use linear, github, jira, or a compiled extension", value)
	}
}

func buildProvider(ctx context.Context, id issueprovider.ID, credential domain.Credential, repo *domain.Repo, env *execEnv) (issueprovider.Provider, error) {
	if extension := env.providers[id]; extension != nil {
		return extension, nil
	}
	switch id {
	case issueprovider.Linear:
		return linearprovider.Client{Credential: credential, HTTPClient: env.http}, nil
	case issueprovider.GitHub:
		repository := strings.TrimSpace(env.env["GITHUB_REPOSITORY"])
		if repository == "" && repo != nil {
			repository = githubRepository(ctx, *repo, env)
		}
		token := strings.TrimSpace(env.env["GITHUB_TOKEN"])
		if token == "" {
			token = strings.TrimSpace(env.env["GH_TOKEN"])
		}
		return githubprovider.New(githubprovider.Options{
			Token: token, APIURL: env.env["GITHUB_API_URL"],
			Repository: repository, HTTPClient: env.http,
		})
	case issueprovider.Jira:
		return jiraprovider.New(jiraprovider.Options{
			BaseURL: env.env["JIRA_BASE_URL"], Email: env.env["JIRA_EMAIL"],
			Token: env.env["JIRA_API_TOKEN"], HTTPClient: env.http,
		})
	default:
		return nil, fmt.Errorf("unsupported issue provider %q", id)
	}
}

func providerDisplayName(id issueprovider.ID, extensions map[issueprovider.ID]issueprovider.Provider) string {
	if extension := extensions[id]; extension != nil {
		if name := strings.TrimSpace(extension.DisplayName()); name != "" {
			return name
		}
		return string(id)
	}
	switch id {
	case issueprovider.GitHub:
		return "GitHub"
	case issueprovider.Jira:
		return "Jira"
	default:
		return "Linear"
	}
}

func githubRepository(ctx context.Context, repo domain.Repo, env *execEnv) string {
	key := branch.RepositoryKey(ctx, repo, env.run)
	parts := strings.Split(key, "/")
	if len(parts) != 3 || !strings.EqualFold(parts[0], "github.com") {
		return ""
	}
	return parts[1] + "/" + parts[2]
}

func preferredRepo(flagRepo, hereRepo *domain.Repo) *domain.Repo {
	if flagRepo != nil {
		return flagRepo
	}
	return hereRepo
}
