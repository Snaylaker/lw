# lw — architecture

Search Linear issues, get a git worktree, and get its path on stdout.

**Linear and git. Nothing else.** No terminal multiplexer, workspace manager, editor,
agent, plugin host, or forge API.

## The flow

```text
inspect cwd/--repo → onboarding (only missing key/folder) → issue search
  → remembered or selected repository → create/reuse worktree → print path
```

Plain `lw` opens one workspace-wide search input:

- `DEMO` lists active issues in the DEMO team.
- `DEMO-4009` resolves the exact active issue.
- `timeout` uses Linear's ranked workspace text search.

`Tab` cycles optional project and team browsers. Each loads one bounded page; selecting a
project or team loads one bounded page of active issues, with local filtering in the scoped
views. Cycling back preserves the previous issue query. Project and team pins persist as
stable IDs and rank matching browser rows first; they are preferences, not list caches.

Search results carry project and team metadata so repository routing stays automatic. A
repository selected for an issue with a project is remembered for that project; a
projectless issue is remembered by team. A stale association falls back to repository
selection.

`--issue` bypasses the TUI. `--repo` bypasses repository selection. Outside a repository,
direct issue mode can use the same remembered project/team association.

## Output contract

The terminal UI ends when the worktree exists, and then the path is printed:

```sh
cd "$(lw)"
cd "$(lw --issue DEMO-4009)"
```

**stdout contains exactly one line: the worktree path.** Bubble Tea and Lip Gloss are both
bound to stderr, so command substitution captures neither escape codes nor progress output.

## Layout

```text
cmd/lw              the only binary
internal/domain     shared project, issue, repository, stage and result values
internal/lwerr      kind + message + next action
internal/config     config.json, durable repository routing, atomic writes
internal/credential resolves, saves and removes the Linear key through a vault boundary
internal/linear     GraphQL transport, exact/team/text search and project/team browsing
internal/gitrepo    repository validation and one-level discovery
internal/worktree   creation, reuse, metadata and pruning
internal/tui        onboarding, issue/project/team views, repository picker and progress
internal/doctor     environment checks
internal/cli        parsing, dispatch and orchestration
```

The TUI receives side effects as callbacks. It never imports the Linear, config or worktree
packages. Tests replace those callbacks; worktree tests use real temporary Git repositories.

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
`~/.lw/worktrees`. The branch is the issue identifier. Re-selecting an issue reuses the
existing worktree.

Metadata lives in the linked worktree's private Git directory as `lw.json`, never in the
checkout:

```json
{
  "identifier": "DEMO-4009",
  "title": "Improve workspace startup prompt",
  "url": "https://linear.app/acme/issue/DEMO-4009",
  "team": "DEMO",
  "summary": ""
}
```

`lw context`, `lw context --json`, and `lw summary` expose or update that local metadata.

## Credentials

Resolution order is `credentialCommand`, `LINEAR_API_KEY`, then onboarding's saved key.
Onboarding validates a masked key, prefers the system keychain, and requires explicit
consent before owner-only file storage. Git and credential-helper children receive an
environment without `LINEAR_API_KEY`.

## Errors and cancellation

Errors are `*lwerr.Error{Kind, Message, NextAction}`. One reporter prints `error: …` and
`next: …`. Exit codes are 0 success, 1 error, 2 usage, and 130 cancellation. Once a
worktree exists it is never rolled back because later presentation was interrupted.

## Non-goals

No Linear mutations, editor or agent launching, commits, pushes, pull requests, plugin
system, daemon, recursive repository search, or runtime dependency beyond git and the
host's optional credential service.
