# lw — specification

Search a Linear issue, choose its source repository, create or reuse a Git worktree, and
print the path.

---

## 1. Scope

```sh
cd "$(lw)"
```

`lw` reads Linear and writes local Git worktrees. It starts no editor, shell or agent,
mutates no Linear data, manages no pull requests, and has no daemon or plugin system. The
only required executable is `git`.

## 2. Commands

| Command | Purpose |
| --- | --- |
| `lw` | workspace-wide interactive issue search |
| `lw doctor` | check the environment |
| `lw branches set-rule/show-rule/preview/unset-rule` | manage one repository's branch rule |
| `lw context [--json]` | print local worktree metadata |
| `lw summary <text>` | update the local summary |
| `lw prune [--yes] [--no-fetch]` | report or remove finished worktrees |
| `lw prune --auto` / `--no-auto` | persist automatic pruning behavior |
| `lw logout` | remove the credential saved by onboarding |

Run flags: `--repo <path>`, `--issue <IDENT>`, `--branch <name>`, `--version`, `--help`.
`--repo` also targets `lw branches`; `--username` is valid only with `branches set-rule`.

Exit codes: `0` success · `1` error · `2` usage · `130` cancelled.

## 3. Run flow

```text
inspect cwd/--repo → conditional onboarding → issue search
  → remembered/selected repository → resolve branch → worktree → stdout path
```

1. Validate an explicit `--repo` and verify Git can run.
2. Resolve credentials; if missing, run credential onboarding.
3. If repository roots are missing, ask for the parent folder containing repositories.
4. Without `--issue`, open workspace-wide issue search; `Tab` cycles optional project and team browsers.
5. Resolve or select the source repository.
6. Fetch and resolve the ticket branch; ask only when the result is ambiguous or a new
   branch needs an editable name.
7. Create/reuse the worktree and write metadata.
8. End the TUI and print exactly one path line to stdout.

`--issue` bypasses every picker. An explicit/current repository wins; outside Git, a valid
remembered project/team association may supply it. Without either, report the standard
not-a-repository error.

Bubble Tea and Lip Gloss both derive output behavior from stderr. stdout must never contain
TUI frames, progress, warnings or errors.

## 4. Interactive issue search

One input supports three modes:

```text
DEMO-4009  exact identifier lookup
DEMO       active issues whose team key is DEMO
demo       the same, case-insensitive
timeout  Linear workspace text search, relevance order preserved
```

- Search starts at two characters.
- Input is debounced for 450ms.
- A late response is ignored when its query is no longer current.
- `Ctrl+R` repeats the current query.
- Exact/team/text results exclude completed and canceled issues.
- A query shaped like a team key first tries the team filter; no team results fall back to
  workspace text search.
- Result rows are `<IDENT> <title>`.
- The hint is `<state> · <project>` when a project exists, otherwise `<state> · <team>`.
- Linear's search relevance order is never locally re-ranked or substring-filtered.

GraphQL:

```graphql
query SearchIssues($term: String!, $first: Int!, $after: String, $filter: IssueFilter) {
  searchIssues(term: $term, first: $first, after: $after, filter: $filter, includeArchived: false) {
    nodes { id identifier title url branchName state { name type } team { id key name } project { id name } }
    pageInfo { hasNextPage endCursor }
  }
}
```

Team and exact lookups use `issues` with `team.key` or `team.key + number` filters.
Workspace and team search each return at most 50 rows per query so typing a team key never
starts a long pagination pass.

### Optional project and team browsers

`Tab` cycles Issues → Projects → Teams → Issues. Each browser loads at most 50 rows and
filters them locally. Selecting a project or team loads at most 50 of its active issues into
a locally filtered issue view. Cycling back preserves the previous issue view and query;
`Esc` from scoped issues returns to its project/team browser. `Ctrl+P` toggles the
highlighted project/team pin; pinned rows appear first and persist by stable Linear ID.
Browsing never changes repository routing: the selected issue's project/team metadata
remains the source of truth.

## 5. Repository selection and routing

Resolution:

1. `--repo` — validated before network work and skips selection.
2. For interactive selection, a valid repository remembered for `issue.project.id`.
3. For a projectless issue, a valid repository remembered for `issue.team.id`.
4. Otherwise the repository picker:
   - current checkout (`●`)
   - recents, newest first
   - repositories directly below configured roots

A project association is more specific and never falls back to a team association for that
same issue. Team associations exist only for projectless issues. Invalid/stale paths reopen
the picker. Explicit selections update recents and the applicable association atomically.

Repository root discovery scans exactly one level. A linked worktree resolves to its main
checkout. An unborn repository fails with `<name> has no commits yet`.

## 6. Branches and worktrees

The worktree identity and Git branch are separate:

- Path: `<worktreeRoot>/<repo name>/<IDENTIFIER>`, default `~/.lw/worktrees`.
- Branch: resolved from repository refs, `--branch`, or that repository's template.

Before creating anything, `lw` fetches `origin` when present and searches local and
`origin/*` refs for the issue identifier, case-insensitively and at non-alphanumeric
boundaries. One match is reused. Several matches open a branch picker. With no match, an
interactive run opens an editable input prefilled from Linear's `branchName` suggestion.
The suggestion starts selected: typing or pasting replaces it, while an arrow key keeps the
value and begins a smaller edit.

`--branch` takes precedence over matching refs. In direct `--issue` mode, creating a branch
requires either `--branch` or a matching repository template; it never silently falls back
to the bare issue identifier. Names are checked with `git check-ref-format --branch`.

A remote-only match becomes a local tracking branch. A new branch starts from the fetched
`origin/HEAD`, then `origin/main` or `origin/master`; a repository without an origin falls
back to its local `HEAD`.

- Existing registered issue worktree: reuse it.
- Existing resolved branch without a worktree: check it out.
- Missing branch: create it with `git worktree add -b` from the resolved base.
- Stale registration: prune and retry once.
- Occupied non-worktree path: fail without deleting it.

Metadata is atomically written mode `0600` to the worktree's private Git directory as
`lw.json`:

```json
{
  "identifier": "DEMO-4009",
  "title": "Improve workspace startup prompt",
  "url": "https://linear.app/acme/issue/DEMO-4009",
  "team": "DEMO",
  "branch": "alex/demo-4009-fix",
  "summary": ""
}
```

Once the checkout exists it is the successful product; later presentation/cancellation does
not roll it back.

## 7. Credentials

One personal Linear key with Read permission. This is intentionally the local personal-script
model: there is no hosted service or shared application credential. Linear recommends OAuth
for hosted applications used by others, so this choice and its limits must remain explicit in
user-facing documentation. Resolution order:

1. `credentialCommand`
2. `LINEAR_API_KEY`
3. system keychain or explicitly approved owner-only fallback file

Onboarding masks and validates the key with `viewer { id }` before saving. Keychain location
is service `lw`, account `linear-api-key`. The fallback directory is `0700`, file `0600`.

The key must never appear in arguments, logs, errors, Git/worktree data or `config.json`.
Credential helpers and Git children receive an environment without `LINEAR_API_KEY`.

## 8. Configuration

```json
{
  "worktreeRoot": "~/.lw/worktrees",
  "credentialCommand": "op read op://private/linear/api-key",
  "repos": {
    "roots": ["~/Work"],
    "recent": [{"path": "~/Work/api", "usedAt": 1785600000000}],
    "projects": [{"projectId": "project-uuid", "path": "~/Work/api", "usedAt": 1785600000000}],
    "teams": [{"teamId": "team-uuid", "path": "~/Work/tools", "usedAt": 1785600000000}]
  },
  "pins": {
    "projects": ["project-uuid"],
    "teams": ["team-uuid"]
  },
  "branchNaming": {
    "variables": {"username": "alex"},
    "byRepository": {
      "gitlab.example.com/group/api": {"template": "{username}/{ticket_lower}-{slug}"}
    }
  },
  "pruneMerged": false
}
```

Config is hand-editable, atomically written, directory `0700`, file `0600`. A malformed
file is an error; malformed entries inside valid JSON are dropped. Preferences are durable
and are not Linear list caches.

Branch rules are repository-scoped. The preferred key is origin normalized to `host/path`;
an absolute checkout path is also accepted for a repository without a usable origin.
Supported placeholders are `{username}`, `{ticket}`, `{ticket_lower}`, `{slug}`, and
`{linear_branch}`. Missing values and unknown placeholders are errors. Templates are data
expanded by `lw`; they are never executed as shell commands.

Rule management is repository-scoped:

```text
lw branches set-rule [--repo <path>] [--username <name>] <template>
lw branches show-rule [--repo <path>]
lw branches preview [--repo <path>] <IDENT>
lw branches unset-rule [--repo <path>]
```

`set-rule`, `show-rule`, and `unset-rule` do not contact Linear. `set-rule` validates both
template syntax and a representative expansion with Git before writing. `preview` resolves
exactly one Linear issue, expands the stored rule, validates the result with
`git check-ref-format --branch`, and prints exactly the resulting branch name. Rule writes
preserve every unrelated config section. Removing a rule keeps the global username variable.

## 9. TUI keys

| Key | Action |
| --- | --- |
| printable input | type into the focused search/input |
| `↑`/`↓` | move |
| `PgUp`/`PgDn` | move by five |
| `Enter` | select/submit |
| `Tab` | cycle Issues → Projects → Teams → Issues |
| `Ctrl+P` | pin/unpin the highlighted project or team |
| `Esc` | scoped issues → their project/team browser; otherwise back/cancel |
| `Ctrl+C` | cancel, exit 130 |
| `Ctrl+R` | repeat issue search or reload the current project/team list |

Progress stages: `preparing` → `creating worktree`. Errors show message, next action, and
retry only for retryable Linear unavailability.

## 10. Pruning and context

`lw context` reads `lw.json`; outside an lw worktree it prints nothing and exits 0.
`lw summary` updates only `summary`. Orphaned metadata is removed only when Git confirms its
branch is gone.

`lw prune` considers lw-owned worktrees whose branch is merged or whose upstream is gone.
It never removes the current checkout, never touches worktrees without `lw.json`, and never
uses `--force`.

## 11. Testing

- No unit test touches real Linear, a real credential or user repositories.
- Git worktree behavior uses temporary real repositories.
- HTTP, vault, clock, command runner and TUI effects are injected.
- TUI behavior is asserted as state transitions.
- Full tests, race tests, vet, formatting and cross-builds must pass before release.

## 12. Presentation contracts

Picker ordering, filtering, status text and key behavior are testable as state without parsing
terminal escape sequences. Package-level Lip Gloss styles resolve the stderr-bound renderer at
render time. The TUI, warnings and errors use stderr; successful run output remains exactly one
worktree path line on stdout.

All public examples and test fixtures use synthetic organizations, projects, teams, issues,
paths and credentials. Environment-specific handoff notes do not belong in the repository.

## 13. Releases

A release is built only from an existing semantic version tag shaped like `vMAJOR.MINOR.PATCH`.
The four prebuilt targets are darwin/amd64, darwin/arm64, linux/amd64 and linux/arm64. Every
archive contains `lw`, `README.md`, `LICENSE` and `THIRD_PARTY_NOTICES.md`. `checksums.txt`
contains one SHA-256 digest per archive and GitHub publishes build-provenance attestations.
Published release assets are immutable and are never overwritten.

Release verification runs module verification, formatting, build, vet, unit tests, race tests,
ShellCheck, generated-notice verification and all four cross-builds. GitHub Actions use pinned
commit SHAs and minimum job permissions.
