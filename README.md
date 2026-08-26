# lw

Search Linear issues, choose or reuse a repository, and get a git worktree path on stdout.
`lw` is one static binary whose only requirement is `git`.

```sh
cd "$(lw)"
```

**`lw` is focused.** It creates a worktree and tells you where it is. It starts no editor,
shell or agent and installs nothing. What happens in the worktree is yours to decide.

`lw` is read-only against Linear. First-run onboarding validates and securely remembers a
Read-only personal API key (see [Connect lw to Linear](#connect-lw-to-linear)).

`lw` is an independent open-source project. It is not affiliated with, endorsed by, or
maintained by Linear.

---

## Install

```sh
curl -fsSL https://raw.githubusercontent.com/snaylaker/lw/main/install.sh | sh
```

The installer detects your OS and architecture, downloads the matching release asset,
**verifies it against the published `checksums.txt` and refuses to install on a
mismatch**, and falls back to `go build` from a shallow clone when no prebuilt asset
matches your platform. It never uses `sudo` and never writes outside the install directory
and a temporary directory it removes on exit. Checksums detect corruption but are published
with the release, so they are not an independent signature. GitHub build-provenance
attestations are also published for release assets and can be checked with
`gh attestation verify <asset> --repo snaylaker/lw`.

Options (pass them after `sh -s --`, or as environment variables):

| Option | Environment | Default | Meaning |
| --- | --- | --- | --- |
| `--version <tag>` | `LW_VERSION` | latest release | install a specific release, e.g. `v0.1.0` |
| `--dir <path>` | `LW_INSTALL_DIR` | `~/.local/bin` | exact directory to install into |
| — | `PREFIX` | — | install into `$PREFIX/bin` |
| `--build-from-source` | `LW_FROM_SOURCE=1` | off | skip the prebuilt asset, build with `go build` |
| — | `LW_REPO` | `snaylaker/lw` | the GitHub repository to install from |
| `--help`, `-h` | — | — | print usage and exit |

```sh
curl -fsSL https://raw.githubusercontent.com/snaylaker/lw/main/install.sh | sh -s -- --version v0.1.0
curl -fsSL https://raw.githubusercontent.com/snaylaker/lw/main/install.sh | LW_INSTALL_DIR=/opt/tools/bin sh
```

The installer prints exactly what it is about to do before it does it, and warns you if
the install directory is not on your `PATH`.

Building it yourself, if you have a Go toolchain:

```sh
git clone https://github.com/snaylaker/lw
cd lw
go build -o ~/.local/bin/lw ./cmd/lw
```

Cross-compiling is the same command with `GOOS` and `GOARCH` set; the result is one static
binary with no runtime dependency.

---

## Connect lw to Linear

`lw` uses one **personal Linear API key with permission `Read`**. There is no OAuth and `lw`
never writes to Linear.

Linear recommends OAuth for hosted applications used by others and personal API keys for
personal scripts. `lw` deliberately follows the personal-script model: it is a local binary,
has no shared backend, and each user supplies and controls their own key. Review this model
against your workspace policy before installing it. See [Linear's authentication
documentation](https://linear.app/developers/graphql#authentication).

When no key is available, the first screen explains where to create one:

```text
Connect to Linear

lw only reads data from Linear.
Create a Personal API key with Read permission:
Settings → Account → Security → Personal API keys
https://linear.app/settings/account/security

Paste key: ••••••••••••
```

The input is masked. `lw` validates the key before saving it. It uses the operating system
credential store—macOS Keychain, Windows Credential Manager, or Linux Secret Service—when
available. On a headless Linux host without a credential store, `lw` validates the key but
asks permission before using a separate owner-only `credentials` file beside `config.json`
(directory mode `0700`, file mode `0600`). It then reports the destination actually used.
The key is never written into `config.json`. The confirmation is explicit:

```text
API key saved in your system keychain.
Service: lw
Account: linear-api-key
```

For the file fallback, the confirmation prints its complete path instead.

Remove the key saved by onboarding with:

```sh
lw logout
```

For CI and password-manager users, existing sources remain available. Resolution order is:

1. **`credentialCommand` in `config.json`** — run through the platform shell. The first line
   of stdout is the key; helper errors and output are never echoed.
2. **`LINEAR_API_KEY` in the environment.**
3. **The credential saved during onboarding.**

### A `credentialCommand` for your secret manager

1Password CLI:

```json
{ "credentialCommand": "op read op://private/linear/api-key" }
```

`pass`:

```json
{ "credentialCommand": "pass show linear/api-key" }
```

Anything else that prints the key on stdout works the same way — `gopass`, `bw get password
linear`, `security find-generic-password -w -s linear`, `cat ~/.secrets/linear` — because
`lw` only reads the first line of its output. This value is executed through the platform
shell, so treat `credentialCommand` as trusted local code and never copy one from an
untrusted source.

### Or just the environment variable

```sh
export LINEAR_API_KEY="lin_api_…"    # in your shell profile, or a direnv .envrc
```

`credentialCommand` wins over the environment, which wins over the saved key.

### The key goes nowhere else

The key is held in memory for credential handling and Linear requests. It never appears in a process argument,
error, log, worktree or `config.json`. Every Git child process receives an environment with
`LINEAR_API_KEY` removed, including processes Git may start. A credential helper receives
the same sanitized environment, and its output is never echoed because that output is the
secret.

`lw` has no telemetry or hosted service. It sends the key and requested search data only to
`https://api.linear.app/graphql`. Local configuration stores repository paths and stable
Linear project/team IDs; each worktree's private Git directory stores its issue identifier,
title, URL, team key, and optional summary.

Error messages from Linear are mapped to fixed strings rather than echoing server text:

| Condition | Kind | Message |
| --- | --- | --- |
| 401/403, or a rejected key | `auth_required` | `Linear rejected the credentials.` |
| 5xx, network failure, timeout | `linear_unavailable` | `Linear is unreachable.` |
| 200 with no viewer | `auth_required` | `Linear rejected the credentials.` |

Each carries a next action, e.g. `create a new Read key and update your credential source`.

---

## The run

```sh
lw
```

1. Inspect `--repo` or the current directory.
2. Complete missing onboarding: connect Linear, then locate the folder containing repositories.
3. Search issues across the workspace, or press `Tab` to browse active projects or teams and their issues.
4. Reuse a remembered repository or ask which repository owns the issue.
5. Create or reuse the worktree and print its path.

The first interactive screen is workspace issue search. `Tab` cycles through **Issues**,
**Projects**, and **Teams**. Selecting a project or team opens its active issues; typing a
team key directly in issue search remains the fastest route:

```text
Find a Linear issue

❯ DEMO

  DEMO-4009  Improve workspace startup prompt       Todo · CLI Reliability
  DEMO-4007  Repository scan timeout     Triage · Developer Experience
```

Search modes are automatic:

| Input | Result |
| --- | --- |
| `DEMO-4009` | resolve that exact active issue |
| `DEMO` or `demo` | list active issues in the DEMO team |
| `timeout` | use Linear's ranked workspace text search |

A team-looking word that is not a real team falls back to text search. Search waits for two
characters, is debounced for 450ms, and ignores responses belonging to an older query.
Finished issues are excluded from interactive results. Project and team browsers each load
up to 50 rows; selecting one loads up to 50 active issues, which are filtered locally as you
type. Cycling back to Issues preserves the previous issue view and query.

A progress view shows `preparing` → `creating worktree`, each pending (`○`), active (`◐`),
done (`●`), failed (`✗`) or skipped (`-`).

**stdout carries exactly one line: the worktree path.** Bubble Tea and Lip Gloss both use
stderr, so command substitution captures only the path:

```sh
cd "$(lw)"                    # interactive issue search
cd "$(lw --issue DEMO-4009)"   # exact issue, no terminal UI
```

Once the worktree exists, nothing invalidates it; the exit code remains `0`.

### Repository routing

1. `--repo <path>` is validated immediately and skips repository selection.
2. An issue with a project reuses the repository last selected for that project.
3. A projectless issue reuses the repository last selected for its team.
4. Otherwise the picker shows the current checkout (`●`), recent choices, then repositories
   found exactly one level below `repos.roots`.

A stale association falls back to the picker. Direct `--issue` mode can use the same
association when run outside Git; if none exists, pass `--repo` or run from a repository.

Onboarding asks **Where are your repositories?** This means their parent directory: for
`~/Work/api` and `~/Work/web`, enter `~/Work`. Linked worktrees resolve to the main checkout.
Repository discovery never scans recursively.

### Keys

| Key | Action |
| --- | --- |
| type | update the focused issue, project, or repository search |
| `↑` / `↓` | move |
| `PgUp` / `PgDn` | move by five |
| `Enter` | select |
| `Tab` | cycle Issues → Projects → Teams → Issues |
| `Esc` | return from project issues to projects, otherwise go back/cancel |
| `Ctrl+C` | cancel with exit 130 |
| `Ctrl+P` | pin/unpin the highlighted project or team |
| `Ctrl+R` | repeat the current issue search or reload the current project/team list |

---

## Commands

| Command | Purpose |
| --- | --- |
| `lw` | workspace-wide interactive issue search; create/reuse a worktree |
| `lw doctor` | check the environment |
| `lw context` | print this worktree's ticket context |
| `lw summary <text>` | record what the work is now about |
| `lw prune` | list worktrees whose branch is merged or gone |
| `lw prune --yes` | remove them |
| `lw prune --auto` / `--no-auto` | save automatic pruning behavior |
| `lw logout` | remove the credential saved during onboarding |

Run flags are `--repo <path>`, `--issue <IDENT>`, `--version`, and `--help`.
`lw context` accepts `--json`; prune accepts `--yes`, `--no-fetch`, `--auto`, and
`--no-auto`.

`--issue` accepts `^([A-Za-z0-9]+)-(\d+)$` and bypasses the TUI:

```sh
lw --issue DEMO-4009
lw --issue DEMO-4009 --repo ~/Work/tools
```

Exit codes are `0` success, `1` error, `2` usage, and `130` cancelled. Errors always carry
a next action.

---

## Where the worktree lands

```
<worktreeRoot>/<repo name>/<IDENTIFIER>
```

with the default `worktreeRoot` of `~/.lw/worktrees` — so issue `ENG-3971` in a repo called
`acme-api` gives you `~/.lw/worktrees/acme-api/ENG-3971`.

**The branch is the issue identifier exactly** (`ENG-3971`). If the branch does not exist it
is created (`git worktree add -b <IDENT> <path>`); if it does, it is checked out
(`git worktree add <path> <IDENT>`).

Picking the same issue again **reuses** the existing worktree instead of nesting a new one.
A stale or prunable registration is cleaned with
`git worktree prune` and retried once. If the path exists but is not a worktree of this
repository:

```
error: <path> already exists and is not a worktree of <repo>
next: remove it, or set worktreeRoot to another location
```

### The metadata file

Each worktree carries its own `lw.json`, written into that worktree's git directory (mode
`0600`, written atomically) and discoverable from any subdirectory of it via
`git rev-parse --git-dir`:

```json
{
  "identifier": "ENG-3971",
  "title": "Improve command completion output",
  "url": "https://linear.app/acme/issue/ENG-3971",
  "team": "ENG",
  "summary": ""
}
```

This file is the product's entire integration surface. It lives outside the working tree,
so it never touches the repository's history or its files.

---

## Configuration

One hand-editable `config.json` is read at run time. It lives in `$LW_CONFIG_DIR` when set,
else `$XDG_CONFIG_HOME/lw`, else the platform config directory.

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
  "pruneMerged": false
}
```

`repos.projects` routes issues that belong to a project. `repos.teams` is used only for
projectless issues. Both are durable preferences learned from explicit repository choices.
`pins.projects` and `pins.teams` hold stable Linear IDs selected with `Ctrl+P`; pinned rows
appear first and survive future runs without caching Linear list data.

Configuration directories created by `lw` use mode `0700`, configuration files use mode
`0600`, and writes are atomic
with two-space JSON and a trailing newline. Mutators re-read immediately before writing,
but separate processes do not hold a cross-process transaction; avoid hand-editing the file
while `lw` is updating it. `credentialCommand` is a command reference, never the key itself.

Malformed JSON is reported rather than treated as an empty configuration:

```text
error: the config file /Users/me/.config/lw/config.json is not valid JSON
next: fix the JSON, or delete the file to start over; your stored API key is unaffected
```

---

## Integration surface

```
lw context           # plain text
lw context --json    # the metadata object verbatim
lw summary "…"       # update the summary field
```

`lw context` prints nothing and exits `0` when the current directory is not a worktree this
tool created, so a caller can run it unconditionally from anywhere.

Plain-text form:

```
Ticket: ENG-3971 — Improve command completion output
https://linear.app/acme/issue/ENG-3971
Summary: <only when non-empty>
This context is read-only; it never writes to Linear.
```

`lw context --json` prints the metadata object verbatim — the same five fields as `lw.json`
— so a program can read it without parsing prose. Neither form talks to Linear, so neither
needs a credential.

### Using it from another tool

Anything that can run a shell command can use this. Point it at `lw context`:

```sh
lw context    # empty and exit 0 outside an lw worktree, so it is safe to run anywhere
```

A tool whose session-start hook is configured as JSON usually wants a command entry:

```json
{
  "hooks": {
    "SessionStart": [
      { "hooks": [{ "type": "command", "command": "lw context" }] }
    ]
  }
}
```

For a consumer that reads structured data, use `lw context --json` and pull `identifier`,
`title`, `url`, `team` and `summary` out of the object.

As the work drifts from the ticket title, record what it is actually about — every later
session start picks it up:

```sh
lw summary "worktree reuse now handles stale registrations"
```

---

## doctor

```sh
lw doctor
```

Each check prints `  ok  `, `warn  ` or `FAIL  `, then `<label>: <detail>`. A failing check
reports its reason as `<message> — next: <next action>`. `lw doctor` exits `1` if any
**mandatory** check fails.

| Check | Mandatory |
| --- | --- |
| platform | yes |
| git | yes |
| current directory is a usable repository | no |
| Linear credential available, and its source | no |
| config file readable | yes |
| worktree root writable | yes |

The credential check reports **which source** would supply the key — `credentialCommand`,
`LINEAR_API_KEY`, system keychain, or owner-only file — which is the fastest way to find out
why a run is picking up the wrong one. The non-mandatory checks are the ones you can legitimately be without: you can run
`lw doctor` outside a repository and without a key.

---

## What lw is not

No workspace manager, multiplexer, or plugin host. No editing another tool's configuration.
`lw` remains one static binary. **No agent launching or lw-specific lifecycle hooks**: after
conditional onboarding and worktree creation, `lw` prints a path and stops. The integration
surface is `lw.json`, `lw context` and `lw summary`, and it is the same for everything.

---

## Contributing and security

See [CONTRIBUTING.md](CONTRIBUTING.md) for the development and verification workflow and
[docs/RELEASING.md](docs/RELEASING.md) for the maintainer release gate. Report vulnerabilities
privately as described in [SECURITY.md](SECURITY.md). Do not put credentials or private Linear
data in an issue or test fixture.

## License

`lw` is licensed under the [Apache License 2.0](LICENSE). Licenses for code linked into the
prebuilt binaries are collected in [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md).
