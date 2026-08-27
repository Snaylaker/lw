# lw

`lw` turns a Linear issue into a Git worktree and prints its path.

```sh
cd "$(lw)"
```

It reads Linear, creates or reuses a local worktree, writes small local metadata, and exits.
It does not start an editor, shell, terminal multiplexer, or coding agent. `lw` is independent
and is not affiliated with or endorsed by Linear.

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

On first use, `lw` asks for:

1. A Linear personal API key with `Read` permission.
2. The parent directory containing your repositories, such as `~/Work`.

The issue picker searches your workspace. Select an issue and repository, then `lw` creates
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
  sh -s -- --version v0.1.0

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

## Find work

Run `lw` for interactive search or pass an exact issue:

```sh
cd "$(lw)"
cd "$(lw --issue DEMO-4009)"
cd "$(lw --issue DEMO-4009 --repo ~/Work/api)"
```

Search behavior:

| Input | Result |
| --- | --- |
| `DEMO-4009` | exact active issue |
| `DEMO` | active issues in that team |
| `timeout` | Linear workspace text search |

`Tab` cycles Issues, Projects, and Teams. `Ctrl+P` pins a project or team. Arrow keys move,
`Enter` selects, `Esc` goes back, `Ctrl+R` reloads, and `Ctrl+C` cancels.

Repository routing remembers the repository selected for a project. Projectless issues use a
team-level association. An explicit `--repo` always wins.

## Commands

| Command | Purpose |
| --- | --- |
| `lw` | select an issue and create or reuse its worktree |
| `lw --issue TEAM-123` | skip the issue picker |
| `lw doctor` | inspect Git, credentials, configuration, and worktree storage |
| `lw context [--json]` | print this worktree's issue context |
| `lw summary <text>` | record how the work has changed from the issue title |
| `lw prune` | show merged or gone worktrees |
| `lw prune --yes` | remove those worktrees without forcing dirty checkouts |
| `lw logout` | remove the credential saved by onboarding |
| `lw --version` | print the installed version |

Successful `lw` output is exactly one path line on stdout. UI, progress, warnings, and errors
use stderr, so command substitution remains safe.

## Coding agents

### Start ticketed work from an agent

If you are a local coding agent reading this, create or reuse the worktree before changing files:

1. Read the exact Linear issue identifier from the user's request. Ask for it if it is missing;
   do not guess.
2. Resolve the current repository root with `git rev-parse --show-toplevel`.
3. Run `lw` non-interactively with both values:

   ```sh
   repo="$(git rev-parse --show-toplevel)"
   worktree="$(lw --issue TEAM-123 --repo "$repo")"
   ```

4. Continue the task from `$worktree` and run `lw context` there before making changes.
5. If your harness cannot change its workspace, give the user the exact command that starts a new
   session of your own agent at the absolute path printed by `lw`:

   ```sh
   cd "/absolute/worktree/path" && <your-agent-command>
   ```

Use your current agent's CLI and preserve any flags the user needs. Replace the placeholders with
real values, and do not launch a nested agent unless the user explicitly asks you to do so.

Every `lw` worktree carries local issue metadata. `lw context` exposes it without contacting
Linear:

```text
Ticket: ENG-3971 - Improve command completion output
https://linear.app/acme/issue/ENG-3971
This context is read-only; it never writes to Linear.
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
  "worktreeRoot": "~/.lw/worktrees",
  "credentialCommand": "op read op://private/linear/api-key",
  "repos": { "roots": ["~/Work"] },
  "pruneMerged": false
}
```

Each created worktree has an `lw.json` in its private Git directory, not in the checkout:

```json
{
  "identifier": "ENG-3971",
  "title": "Improve command completion output",
  "url": "https://linear.app/acme/issue/ENG-3971",
  "team": "ENG",
  "summary": ""
}
```

`lw` has no telemetry or hosted service. Network requests go to Linear's documented GraphQL
endpoint and, during installation, GitHub. Git children do not inherit `LINEAR_API_KEY`.

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
