# lw — architecture

Search issues, get a Git worktree, and get its path on stdout.

**Read-only issue providers and Git. Nothing else.** No terminal multiplexer, workspace manager,
editor, agent, runtime plugin host, or provider mutation API.

## The flow

```text
inspect cwd/--repo → provider selection/auth → issue search
  → remembered or selected repository → resolve branch → create/reuse worktree → print path
```

Plain `lw` opens one search input for the selected provider. Linear supports:

- `DEMO` lists active issues in the DEMO team.
- `DEMO-4009` resolves the exact active issue.
- `timeout` uses Linear's ranked workspace text search.

For Linear, `Tab` cycles optional project and team browsers. Each loads one bounded page; selecting a
project or team loads one bounded page of active issues, with local filtering in the scoped
views. Cycling back preserves the previous issue query. Project and team pins persist as
stable IDs and rank matching browser rows first; they are preferences, not list caches.

Search results carry neutral provider scopes so repository routing stays automatic. For Linear,
a repository selected for an issue with a project is remembered for that project; a projectless
issue is remembered by team. A stale association falls back to repository selection.

`--issue` bypasses the TUI. `--repo` bypasses repository selection. Outside a repository,
direct issue mode can use the same remembered project/team association.

## Output contract

The terminal UI ends when the worktree exists, and then the path is printed:

```sh
cd "$(lw)"
cd "$(lw --issue DEMO-4009 --branch alex/demo-4009-fix)"
```

**stdout contains exactly one line: the worktree path.** Bubble Tea and Lip Gloss are both
bound to stderr, so command substitution captures neither escape codes nor progress output.

## Layout

```text
lw.go               public Run entry point for custom binaries
cmd/lw              the official binary
provider             public compile-time provider contract and neutral WorkItem
internal/providers   adapters and Linear/GitHub/Jira implementations
internal/domain     execution values and the canonical WorkItem alias
internal/lwerr      kind + message + next action
internal/config     config.json, repository routing, branch rules and locked transactions
internal/credential resolves, saves and removes the Linear key through a vault boundary
internal/processenv removes provider secrets from child-process environments
internal/linear     Linear GraphQL transport and optional project/team capabilities
internal/gitrepo    repository validation and one-level discovery
internal/branch     ref discovery, template expansion and branch planning
internal/worktree   creation, reuse, metadata and pruning
internal/tui        onboarding, issue/project/team/repository/branch views and progress
internal/doctor     environment checks
internal/cli        parsing, dispatch and orchestration
```

The TUI receives side effects as callbacks. It never imports the Linear, config or worktree
packages. Tests replace those callbacks; worktree tests use real temporary Git repositories.

## Provider boundary

The public `provider.Provider` interface exposes only identity, reference validation, exact
resolution, and search. It returns a neutral `provider.WorkItem` containing separate human
reference, filesystem-safe worktree key, and durable routing scopes. A validated registry rejects
non-canonical, duplicate, and built-in-colliding custom IDs before provider work starts. Providers
cannot access Git, config, TUI, or worktree operations.

```text
Linear GraphQL ─┐
GitHub REST ────┼→ validate provider.WorkItem → branch → worktree
Jira REST ──────┘
```

`internal/providers` validates and normalizes provider output without changing its neutral
scopes. Those scopes feed the same repository-routing store: Linear
project/team, GitHub repository, or Jira project. The released binary wires three built-ins; the
public Go interface supports custom builds through the root `lw.Run` API, not runtime plugin
loading. Providers that implement `SensitiveEnvironmentProvider` declare credential variables;
the registry combines them with built-in secrets before creating Git or `lw run` children.

Provider choice is an explicit reference prefix, `--provider`, `LW_ISSUE_PROVIDER`, config, then
Linear. GitHub and Jira use environment credentials; only Linear has interactive onboarding and
collection browsers.

## Linear search

Interactive search waits for at least two characters and debounces requests for 450ms.
Late responses are ignored when the query has changed.

```text
TEAM-123  → exact issue lookup
TEAM      → active issues filtered by team key
anything  → Linear searchIssues relevance order
```

An uppercase word that is not a team falls back to text search. Finished issues are excluded
from interactive results. Exact `--issue` remains available for scripts.

## Repository routing

Config stores durable routing under `repos`:

```json
{
  "repos": {
    "roots": ["~/Work"],
    "recent": [{"path": "~/Work/api", "usedAt": 1785600000000}],
    "projects": [{"projectId": "project-uuid", "path": "~/Work/api", "usedAt": 1785600000000}],
    "teams": [{"teamId": "team-uuid", "path": "~/Work/tools", "usedAt": 1785600000000}]
  },
  "pins": {
    "projects": ["project-uuid"],
    "teams": ["team-uuid"]
  }
}
```

Project routing is more specific. Team routing is recorded and consulted only for issues
without a project. Repository roots are scanned exactly one level deep.

## Worktrees

A worktree lives at `<worktreeRoot>/<repo name>/<IDENTIFIER>`, default
`~/.lw/worktrees`. Its directory keeps the stable issue identifier while its Git branch
follows the repository's convention. GitHub references such as `owner/repo#42` use a safe
worktree key such as `GH-owner-repo-42`; the original reference remains in metadata.

After repository selection, `internal/branch` fetches origin, searches local and remote refs
for the ticket, validates names with Git, and returns a plan. One existing match is automatic;
several open a picker. With no match, interactive mode edits the provider suggestion while direct
mode requires `--branch` or a repository template. New branches start from the fetched remote
default branch. Re-selecting an issue reuses its existing worktree.

`lw branches` manages these repository-scoped rules through config's locked read-modify-write
transactions. Rule lookup and mutation use the same normalized-origin identity as the worktree flow. `preview`
is the only management action that contacts the selected provider; it expands the real issue and delegates
final ref validation to Git.

Metadata lives in the linked worktree's private Git directory as `lw.json`, never in the
checkout:

```json
{
  "identifier": "DEMO-4009",
  "provider": "linear",
  "externalId": "issue-uuid",
  "reference": "DEMO-4009",
  "title": "Improve workspace startup prompt",
  "url": "https://linear.app/acme/issue/DEMO-4009",
  "team": "DEMO",
  "branch": "alex/demo-4009-fix",
  "summary": ""
}
```

`lw context`, `lw context --json`, and `lw summary` expose or update that local metadata.

## Credentials

Linear resolves `credentialCommand`, `LINEAR_API_KEY`, then onboarding's saved key. GitHub reads
`GITHUB_TOKEN` or `GH_TOKEN`, with unauthenticated public access as a fallback. Jira Cloud reads
its URL, email, and API token from environment variables. Only Linear persists credentials.
Git children and launched commands receive no provider API token.

## Errors and cancellation

Errors are `*lwerr.Error{Kind, Message, NextAction}`. One reporter prints `error: …` and
`next: …`. Exit codes are 0 success, 1 error, 2 usage, and 130 cancellation. Once a
worktree exists it is never rolled back because later presentation was interrupted.

## Non-goals

No provider mutations, editor or agent launching, commits, pushes, pull requests, runtime plugin
system, daemon, recursive repository search, or runtime dependency beyond git and the
host's optional credential service.
