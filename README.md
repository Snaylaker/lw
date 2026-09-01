# lw

`lw` turns a Linear, GitHub, or Jira issue into a Git worktree and prints its path.

```sh
cd "$(lw)"
```

It reads the selected issue provider, creates or reuses a local worktree, writes small local
metadata, and exits. It does not start an editor, shell, terminal multiplexer, or coding agent.
`lw` is independent and is not affiliated with or endorsed by Linear, GitHub, or Atlassian.

## The app

![Search GitHub issues in lw](docs/images/provider-search.png)

| Repository branch naming | Worktree creation |
| --- | --- |
| ![Edit the suggested branch name](docs/images/branch-name.png) | ![Create the isolated worktree](docs/images/worktree-ready.png) |

The screenshots use deterministic synthetic data rendered from the real TUI views. See
[`docs/images`](docs/images/README.md) for the reproducible capture command.

## Supported issue providers

| Provider | Interactive search | Direct reference | Authentication |
| --- | --- | --- | --- |
| Linear | workspace, team, project, and team browsers | `ENG-3971` or `linear:ENG-3971` | onboarding, `LINEAR_API_KEY`, or `credentialCommand` |
| GitHub | open issues; pull requests excluded | `github:owner/repository#42` | `GITHUB_TOKEN`, `GH_TOKEN`, or public access |
| Jira Cloud | active issues through enhanced JQL | `jira:OPS-42` | `JIRA_BASE_URL`, `JIRA_EMAIL`, and `JIRA_API_TOKEN` |
| Custom build | provider-defined | `--provider <id> --issue <reference>` | provider-defined |

Every provider enters the same repository routing, branch discovery, worktree, metadata, context,
pruning, and command-launch flow.

## Flagship features

### 1. Turn an issue into an isolated checkout

Pick an issue and `lw` creates or reuses a dedicated Git worktree for it:

```sh
cd "$(lw)"
```

Worktrees have predictable paths, separate from the repository's main checkout:

```text
~/.lw/worktrees/<repository>/<ISSUE>
```

### 2. Find work quickly

The interactive picker searches the selected provider. Linear also supports team lookup and
`Tab` browsers for projects and teams, while `Ctrl+P` pins those collections. To bypass
the picker, provide an issue directly:

```sh
cd "$(lw --issue TEAM-123 --branch alex/team-123-fix)"
```

### 3. Use each repository's branch convention

Before creating a branch, `lw` fetches origin and looks for local or remote branches that
already contain the ticket identifier. It reuses one clear match, asks when several match,
and otherwise offers the provider's suggested branch name, or a generated fallback, as editable
text. The worktree directory identity stays separate from the branch name.

### 4. Route issues to the right repository

`lw` remembers the repository selected for a durable provider scope: a Linear project or team,
a GitHub repository, or a Jira project. An explicit repository always takes precedence:

```sh
cd "$(lw --issue TEAM-123 --repo ~/src/api --branch alex/team-123-fix)"
```

### 5. Compose with shells and other tools

On success, `lw` prints exactly one path on stdout. Pickers, progress, warnings, and errors go to
stderr, so command substitution remains predictable and scripts can consume the result safely.

### 6. Carry ticket context into the worktree

Every worktree stores small local metadata for its issue. Read it without another provider request,
or add a local summary when the focus changes:

```sh
lw context
lw context --json
lw summary "investigate the repository discovery failure"
```

These commands update local context only; they never write to an issue provider.

### 7. Clean up worktrees safely

Cleanup is a two-step operation. First inspect worktrees whose branches are merged or gone, then
remove them explicitly:

```sh
lw prune
lw prune --yes
```

Dirty worktrees are not force-removed. Automatic pruning is opt-in.

### 8. Keep credentials and data local

All provider access is read-only. Linear keeps its system-keychain onboarding; GitHub and Jira use
standard environment variables. `lw` has no hosted service or telemetry and never writes to an
issue provider. Run `lw doctor` to inspect the active provider configuration.

## Quick start

Install the latest release on macOS or Linux:

```sh
curl -fsSL https://raw.githubusercontent.com/Snaylaker/lw/main/install.sh | sh
```

Then run it inside any Git repository:

```sh
cd ~/Work/api
cd "$(lw)"
```

Linear is the default provider. On its first use, `lw` asks for a Read-only Linear personal API
key. GitHub and Jira use the environment variables in [Choose an issue provider](#choose-an-issue-provider).
When repository roots are not configured, `lw` also asks for the parent directory containing your
repositories, such as `~/Work`.

The issue picker searches the selected provider. Select an issue and repository, then `lw` creates
or reuses:

```text
~/.lw/worktrees/<repository>/<ISSUE>
```

## Install

The installer selects a release for these targets:

| OS | Architectures |
| --- | --- |
| macOS | Intel, Apple Silicon |
| Linux | AMD64, ARM64 |

It verifies the archive against `checksums.txt`, installs to `~/.local/bin/lw`, never uses
`sudo`, and falls back to a source build when no release matches. A source build needs Go:

```sh
git clone https://github.com/Snaylaker/lw
cd lw
go build -o ~/.local/bin/lw ./cmd/lw
```

Useful installer options:

```sh
# Install a specific release
curl -fsSL https://raw.githubusercontent.com/Snaylaker/lw/main/install.sh |
  sh -s -- --version v0.4.0

# Choose the destination
curl -fsSL https://raw.githubusercontent.com/Snaylaker/lw/main/install.sh |
  LW_INSTALL_DIR=/opt/tools/bin sh

# Build instead of downloading an artifact
curl -fsSL https://raw.githubusercontent.com/Snaylaker/lw/main/install.sh |
  sh -s -- --build-from-source
```

Release artifacts also carry GitHub build-provenance attestations:

```sh
gh attestation verify <asset> --repo Snaylaker/lw
```

## Connect Linear

`lw` uses your own Read-only Linear personal API key. It never writes to Linear. Credential
resolution order is:

1. `credentialCommand` in `config.json`
2. `LINEAR_API_KEY`
3. The key saved during onboarding

Onboarding validates the key, then prefers macOS Keychain, Windows Credential Manager, or
Linux Secret Service. If no credential store is available, it asks before using a separate
owner-only file. The key is never written to `config.json`.

Examples for existing secret managers:

```json
{ "credentialCommand": "op read op://private/linear/api-key" }
```

```sh
export LINEAR_API_KEY="lin_api_..."
```

`credentialCommand` runs through the platform shell. Treat it as trusted local code.
Remove a key saved by onboarding with `lw logout`.

Linear recommends OAuth for hosted applications used by others. `lw` deliberately uses the
personal-script model: it has no shared backend, and every user controls their own local key.

## Choose an issue provider

Linear remains the default. Select a provider for one run with `--provider`, prefix a direct
reference, set `LW_ISSUE_PROVIDER`, or put `"issueProvider"` in `config.json`:

```sh
lw --provider github
lw --issue 'github:owner/repository#42' --branch alex/gh-42-fix
lw --provider jira --issue OPS-42 --branch alex/ops-42-fix
```

GitHub uses `GITHUB_TOKEN` or `GH_TOKEN` when present. Public issues can be read without a token;
a token enables private repositories and a higher API rate limit. `GITHUB_API_URL` supports GitHub
Enterprise and `GITHUB_REPOSITORY=owner/repository` supplies context for short references such as
`#42`.

Jira Cloud requires:

```sh
export JIRA_BASE_URL="https://example.atlassian.net"
export JIRA_EMAIL="alex@example.com"
export JIRA_API_TOKEN="..."
```

`lw run` removes `LINEAR_API_KEY`, `GITHUB_TOKEN`, `GH_TOKEN`, and `JIRA_API_TOKEN` from the
launched command's environment. GitHub and Jira credentials are not stored by `lw`.

## Provider interface

The public Go package `github.com/snaylaker/lw/provider` exposes the compile-time `Provider`
interface and neutral `WorkItem` contract. Provider implementations resolve and search issues;
they never receive Git or worktree access. The released binary includes Linear, GitHub, and Jira.
This is not a runtime plugin protocol. A custom binary supplies implementations through
`lw.Run(args, lw.Options{Providers: []provider.Provider{myProvider}})`; `--provider` then selects
the implementation by its ID.

## Find work

Run `lw` for interactive search or pass an exact issue:

```sh
cd "$(lw)"
cd "$(lw --issue DEMO-4009 --branch alex/demo-4009-fix)"
cd "$(lw --issue DEMO-4009 --repo ~/Work/api --branch alex/demo-4009-fix)"
cd "$(lw --issue 'github:owner/repository#42' --branch alex/gh-42-fix)"
cd "$(lw --provider jira --issue OPS-42 --branch alex/ops-42-fix)"
```

Linear search behavior:

| Input | Result |
| --- | --- |
| `DEMO-4009` | exact active issue |
| `DEMO` | active issues in that team |
| `timeout` | Linear workspace text search |

For Linear, `Tab` cycles Issues, Projects, and Teams and `Ctrl+P` pins a project or team.
GitHub and Jira use one provider-ranked issue search. Arrow keys move,
`Enter` selects, `Esc` goes back, `Ctrl+R` reloads, and `Ctrl+C` cancels.

Repository routing remembers the repository selected for a project. Projectless issues use a
team-level association. An explicit `--repo` always wins.

After choosing the repository, `lw` fetches origin and searches local and remote branches for
the ticket identifier. One match is reused; several open a branch picker. With no match, the
interactive flow selects the provider suggestion or generated fallback so typing immediately replaces it; use an
arrow key first to keep and edit the suggestion. Direct `--issue` mode requires
`--branch` before creating a branch unless that repository has a configured template:

```sh
lw --issue DEMO-4009 --branch alex/demo-4009-fix
```

Manage a repository rule without editing JSON:

```sh
lw branches set-rule --username alex '{username}/{ticket}/{slug}'
lw branches show-rule
lw branches preview DEMO-4009
lw branches unset-rule
```

Run these inside the repository, or add `--repo ~/Work/api`. `--username` updates the shared
explicit username variable. `preview` accepts `--provider` or a prefixed reference, resolves the
issue, expands every placeholder, and asks Git to validate the resulting branch name.

## Commands

| Command | Purpose |
| --- | --- |
| `lw` | select an issue and create or reuse its worktree |
| `lw --issue TEAM-123 --branch <name>` | skip the issue picker and name a new branch |
| `lw --provider github` / `jira` | search another built-in provider |
| `lw doctor` | inspect Git, credentials, configuration, and worktree storage |
| `lw branches set-rule <template>` | save the current repository's naming rule |
| `lw branches show-rule` | show the current repository's rule |
| `lw branches preview TEAM-123` | expand and validate the rule for an issue |
| `lw branches unset-rule` | remove the current repository's rule |
| `lw context [--json]` | print this worktree's issue context |
| `lw summary <text>` | record how the work has changed from the issue title |
| `lw prune` | show merged or gone worktrees |
| `lw prune --yes` | remove those worktrees without forcing dirty checkouts |
| `lw logout` | remove the credential saved by onboarding |
| `lw --version` | print the installed version |

Successful `lw` output is exactly one path line on stdout. UI, progress, warnings, and errors
use stderr, so command substitution remains safe.

## Coding agents

Every `lw` worktree carries local issue metadata. `lw context` exposes it without contacting
the provider:

```text
Ticket: ENG-3971 - Improve command completion output
https://linear.app/acme/issue/ENG-3971
This context is read-only; it never writes to the issue provider.
```

Use the agent-specific setup in [the integration guide](docs/agent-integrations.md):

- [Claude Code](docs/agent-integrations.md#claude-code)
- [Cursor](docs/agent-integrations.md#cursor)
- [OpenAI Codex](docs/agent-integrations.md#openai-codex)
- [Gemini CLI](docs/agent-integrations.md#gemini-cli)
- [GitHub Copilot](docs/agent-integrations.md#github-copilot)
- [Windsurf and Devin](docs/agent-integrations.md#windsurf-and-devin)
- [Other agents](docs/agent-integrations.md#other-agents)

Agents only receive the context you explicitly inject or ask them to read. Review issue titles,
URLs, and summaries before sending them to a hosted model.

## Local data

The config file lives in `$LW_CONFIG_DIR`, `$XDG_CONFIG_HOME/lw`, or the platform config
directory. A minimal example:

```json
{
  "issueProvider": "linear",
  "worktreeRoot": "~/.lw/worktrees",
  "credentialCommand": "op read op://private/linear/api-key",
  "repos": { "roots": ["~/Work"] },
  "branchNaming": {
    "variables": { "username": "alex" },
    "byRepository": {
      "gitlab.example.com/group/api": {
        "template": "{username}/{ticket_lower}-{slug}"
      }
    }
  },
  "pruneMerged": false
}
```

Branch rules are keyed by the normalized origin (`host/path`). For a local-only repository,
you can use its absolute checkout path as the key. Templates support `{username}`, `{ticket}`,
`{ticket_lower}`, `{slug}`, `{suggested_branch}`, and the backward-compatible
`{linear_branch}` alias. They are expanded as data and never run as
shell commands. The `branches` commands identify the repository from its normalized origin and
fall back to its absolute path when it has no origin.

Each created worktree has an `lw.json` in its private Git directory, not in the checkout:

```json
{
  "identifier": "ENG-3971",
  "provider": "linear",
  "externalId": "issue-uuid",
  "reference": "ENG-3971",
  "title": "Improve command completion output",
  "url": "https://linear.app/acme/issue/ENG-3971",
  "team": "ENG",
  "branch": "alex/eng-3971-fix",
  "summary": ""
}
```

`lw` has no telemetry or hosted service. Network requests go only to the selected issue provider,
the selected repository's `origin` during branch resolution, and GitHub during installation.
Git children and launched commands do not inherit provider API tokens.

## Documentation

- [Architecture](ARCHITECTURE.md)
- [Behavioral specification](SPEC.md)
- [Agent integrations](docs/agent-integrations.md)
- [Contributing](CONTRIBUTING.md)
- [Security policy](SECURITY.md)
- [Release process](docs/RELEASING.md)

## License

`lw` is licensed under the [Apache License 2.0](LICENSE). Dependency licenses are collected
in [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md).
