# lw — specification

Search a Linear, GitHub, or Jira issue, choose its source repository, create or reuse a Git
worktree, and print the path.

---

## 1. Scope

```sh
cd "$(lw)"
```

`lw` reads one selected issue provider and writes local Git worktrees. It starts no editor,
shell or agent, mutates no provider data, manages no pull requests, and has no daemon or runtime
plugin system. The only required executable is `git`. The released binary contains read-only
Linear, GitHub, and Jira Cloud providers.

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

Run flags: `--repo <path>`, `--issue <REFERENCE>`, `--provider <name>`, `--branch <name>`,
`--version`, `--help`. `--repo` and `--provider` also target `lw branches preview`;
`--username` is valid only with `branches set-rule`.

Exit codes: `0` success · `1` error · `2` usage · `130` cancelled.

## 3. Run flow

```text
inspect cwd/--repo → select provider → conditional onboarding → issue search
  → remembered/selected repository → resolve branch → worktree → stdout path
```

1. Validate an explicit `--repo` and verify Git can run.
2. Select `linear`, `github`, or `jira`; resolve that provider's read-only credentials.
3. If repository roots are missing, ask for the parent folder containing repositories.
4. Without `--issue`, open provider issue search; Linear additionally offers project and team browsers.
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

## 4. Issue providers and interactive search

Provider resolution order is a prefix on the direct reference (`github:owner/repo#42`),
`--provider`, `LW_ISSUE_PROVIDER`, `issueProvider` in config, then `linear`. A prefix and flag
that disagree are a usage error. Non-Linear direct references should use a prefix or explicit
flag so parsing does not depend on configuration.

Every provider implements the public compile-time interface in `provider.Provider`: stable ID,
display name, reference validation, exact resolution, and search returning neutral `WorkItem`
values. Provider IDs must be canonical and unique; custom providers cannot shadow built-ins.
Providers never receive Git or worktree operations. A provider that reads credentials from the
environment implements `provider.SensitiveEnvironmentProvider` so `lw` can remove those variables
from Git children and launched commands. Custom binaries pass implementations to the public
`lw.Run` entry point; the interface is not runtime plugin discovery.

Linear's one-input search supports three modes:

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

GitHub uses REST issue search, adds `is:issue`, and excludes pull requests. With a GitHub
repository context, `#42` is accepted; otherwise use `owner/repository#42`. Jira Cloud uses the
enhanced JQL search endpoint and issue keys such as `OPS-42`. Both return at most 20 rows.

### Optional Linear project and team browsers

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
2. For interactive selection, a valid repository remembered for the issue's most specific
durable provider scope: Linear project/team, GitHub repository, or Jira project.
4. Otherwise the repository picker:
   - current checkout (`●`)
   - recents, newest first
   - repositories directly below configured roots

For Linear, a project association is more specific and never falls back to a team association
for that same issue. Team associations exist only for projectless issues. Invalid/stale paths reopen
the picker. Explicit selections update recents and the applicable association atomically.

Repository root discovery scans exactly one level. A linked worktree resolves to its main
checkout. An unborn repository fails with `<name> has no commits yet`.

## 6. Branches and worktrees

The worktree identity and Git branch are separate:

- Path: `<worktreeRoot>/<repo name>/<WORKTREE_KEY>`, default `~/.lw/worktrees`.
- Branch: resolved from repository refs, `--branch`, or that repository's template.

Before creating anything, `lw` fetches `origin` when present and searches local and
`origin/*` refs for the provider's safe worktree key and branch-match keys, case-insensitively
and at non-alphanumeric boundaries. One match is reused. Several matches open a branch picker.
With no match, an interactive run opens an editable input prefilled from the provider suggestion
or a generated `<worktree-key>-<title-slug>` fallback.
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
- Failed `git worktree add`: preserve Git's failure without mutating unrelated registrations.
- Occupied non-worktree path: fail without deleting it.

Metadata is atomically written mode `0600` to the worktree's private Git directory as
`lw.json`:

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

Once the checkout exists it is the successful product; later presentation/cancellation does
not roll it back.

## 7. Credentials

All provider access is read-only and local to the user:

- Linear: `credentialCommand`, then `LINEAR_API_KEY`, then system keychain or explicitly
  approved owner-only fallback file. Interactive onboarding masks and validates the key.
- GitHub: `GITHUB_TOKEN`, then `GH_TOKEN`; no token permits public issue access. Optional
  `GITHUB_API_URL` and `GITHUB_REPOSITORY` configure Enterprise or short-reference context.
- Jira Cloud: `JIRA_BASE_URL`, `JIRA_EMAIL`, and `JIRA_API_TOKEN`, using HTTPS Basic auth.

Linear keychain location is service `lw`, account `linear-api-key`. The fallback directory is
`0700`, file `0600`. GitHub and Jira credentials are not persisted by `lw`.

No secret may appear in arguments, logs, errors, Git/worktree data or `config.json`.
Credential helpers, Git children, and launched commands receive an environment without `LINEAR_API_KEY`,
`GITHUB_TOKEN`, `GH_TOKEN`, or `JIRA_API_TOKEN`.

## 8. Configuration

```json
{
  "issueProvider": "linear",
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

Config is hand-editable, atomically written, directory `0700`, file `0600`. Mutations lock the
config across processes for the full read-modify-write transaction. A malformed file is an error;
malformed entries inside valid JSON are dropped, while unknown keys survive rewrites for forward
compatibility. Preferences are durable and are not Linear list caches.

Branch rules are repository-scoped. The preferred key is origin normalized to `host/path`;
an absolute checkout path is also accepted for a repository without a usable origin.
Supported placeholders are `{username}`, `{ticket}`, `{ticket_lower}`, `{slug}`, and
`{suggested_branch}`. `{linear_branch}` remains a backward-compatible alias. Missing values and
unknown placeholders are errors. Templates are data
expanded by `lw`; they are never executed as shell commands.

Rule management is repository-scoped:

```text
lw branches set-rule [--repo <path>] [--username <name>] <template>
lw branches show-rule [--repo <path>]
lw branches preview [--repo <path>] <IDENT>
lw branches unset-rule [--repo <path>]
```

`set-rule`, `show-rule`, and `unset-rule` do not contact a provider. `set-rule` validates both
template syntax and a representative expansion with Git before writing. `preview` resolves
exactly one issue from the selected provider, expands the stored rule, validates the result with
`git check-ref-format --branch`, and prints exactly the resulting branch name. Rule writes
preserve every unrelated config section. Removing a rule keeps the global username variable.

## 9. TUI keys

| Key | Action |
| --- | --- |
| printable input | type into the focused search/input |
| `↑`/`↓` | move |
| `PgUp`/`PgDn` | move by five |
| `Enter` | select/submit |
| `Tab` | Linear only: cycle Issues → Projects → Teams → Issues |
| `Ctrl+P` | pin/unpin the highlighted project or team |
| `Esc` | scoped issues → their project/team browser; otherwise back/cancel |
| `Ctrl+C` | cancel, exit 130 |
| `Ctrl+R` | repeat issue search or reload the current project/team list |

Progress stages: `preparing` → `creating worktree`. Errors show message, next action, and
retry only for retryable provider unavailability.

## 10. Pruning and context

`lw context` reads `lw.json`; outside an lw worktree it prints nothing and exits 0.
`lw summary` updates only `summary`. Orphaned metadata is removed only when Git confirms its
branch is gone.

`lw prune` considers lw-owned worktrees whose branch is merged or whose upstream is gone.
It never removes the current checkout, never touches worktrees without `lw.json`, and never
uses `--force`.

## 11. Testing

- No unit test touches a real issue provider, credential, or user repository.
- Git worktree behavior uses temporary real repositories.
- HTTP, vault, clock, command runner and TUI effects are injected.
- TUI behavior is asserted as state transitions.
- Full tests, race tests, vet, formatting and cross-builds must pass before release.

## 12. Presentation contracts

Reported error kinds are `auth_required`, `linear_unavailable` (retained for Linear compatibility),
`provider_unavailable`, `not_a_repo`, `config_invalid`, `worktree_conflict`, `cancelled`, and
`internal`. Every non-cancelled error includes a next action.

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
