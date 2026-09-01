# AGENTS.md

## Project overview

`lw` is a Go CLI that turns a Linear, GitHub, or Jira issue into an isolated Git worktree. It can either print
the worktree path for shell composition or run an explicit command inside the worktree.

The normal flow is:

```text
issue search -> repository routing -> branch resolution -> create or reuse worktree -> path or command
```

`lw` is local-first. It reads one selected issue provider, writes only local configuration and Git worktrees, and
has no hosted service or telemetry.

## Flagship features

1. **Issue-based worktrees**
   - Search the selected provider interactively or pass a provider-specific `--issue` reference.
   - Create worktrees under `~/.lw/worktrees/<repository>/<ISSUE>` by default.
   - Keep the issue identifier as the worktree directory while resolving repository-specific branch names.

2. **Interactive issue discovery**
   - Search by exact issue identifier, team key, or workspace text.
   - Use `Tab` to cycle through issue, project, and team views.
   - Use `Ctrl+P` to pin frequently used projects or teams.

3. **Repository routing**
   - Remember which local repository belongs to a Linear project.
   - Use team-level routing for issues without a project.
   - Let an explicit `--repo` override remembered routing.

4. **Repository-specific branches**
   - Fetch and reuse one local or remote branch that unambiguously contains the ticket identifier.
   - Prompt when several branches match, or edit the provider suggestion when no branch exists.
   - Use safe repository-scoped templates or `--branch` for non-interactive creation.

5. **Shell composition**
   - Plain `lw` prints exactly one worktree path on stdout.
   - Pickers, progress, warnings, and errors go to stderr.
   - This makes `cd "$(lw)"` and scripts predictable.

6. **Explicit command launching**
   - `lw run -- <command> [args...]` runs a command directly inside the selected worktree.
   - It does not invoke a shell or reinterpret arguments.
   - The child owns stdin, stdout, stderr, and its exit code.
   - The child inherits the user environment except provider API tokens.

7. **Local ticket context**
   - Each worktree stores `lw.json` in its private Git directory, not in the checkout.
   - `lw context` and `lw context --json` expose that metadata without contacting a provider.
   - `lw summary <text>` records a local change of focus without writing to a provider.

8. **Safe cleanup and diagnostics**
   - `lw prune` previews merged or gone worktrees.
   - `lw prune --yes` removes eligible worktrees without forcing dirty checkouts.
   - `lw doctor` checks Git, credentials, configuration, and worktree storage.

9. **Local credential handling**
   - Every provider integration is read-only.
   - Linear credential resolution is `credentialCommand`, then `LINEAR_API_KEY`, then the saved key.
   - Saved Linear credentials prefer the operating system credential store.
   - GitHub reads `GITHUB_TOKEN`/`GH_TOKEN`; Jira reads its URL, email, and token from environment variables.

## How to use `lw`

Pick an issue interactively and enter its worktree:

```sh
cd "$(lw)"
```

Resolve an issue directly without the picker:

```sh
cd "$(lw --issue TEAM-123 --branch alex/team-123-fix)"
```

Force the source repository:

```sh
cd "$(lw --issue TEAM-123 --repo ~/src/api --branch alex/team-123-fix)"
```

Manage the current repository's branch rule:

```sh
lw branches set-rule --username alex '{username}/{ticket}/{slug}'
lw branches show-rule
lw branches preview TEAM-123
lw branches unset-rule
```

Run an explicit command inside the worktree:

```sh
lw run -- pi
lw run -- claude
lw run -- cursor .
```

Inspect or update local issue context from inside an `lw` worktree:

```sh
lw context
lw context --json
lw summary "investigate repository discovery"
```

Preview and perform cleanup:

```sh
lw prune
lw prune --yes
```

## Product boundaries

Preserve these contracts when changing the code:

- Do not mutate issue-provider data.
- Do not add a hosted backend, daemon, telemetry, or hidden network service.
- Do not couple core behavior to an editor, agent, terminal multiplexer, or workspace manager.
- Keep plain `lw` stdout to exactly one successful path line.
- Keep TUI output, progress, warnings, and errors on stderr.
- Never expose credentials in arguments, logs, errors, config, metadata, or child processes.
- Never delete an occupied non-worktree path or force-remove a dirty worktree.
- Do not place `lw.json` in the repository checkout.
- Use synthetic organizations, issues, URLs, paths, and credentials in documentation and tests.

## Source map

```text
lw.go               public custom-binary Run entry point
cmd/lw              official binary entry point
internal/cli         parsing, dispatch, orchestration, and child launching
internal/domain      shared issue, repository, stage, and result values
internal/config      local config, repository routing, and branch templates
internal/credential  credential resolution and storage boundaries
provider             public compile-time provider and WorkItem contract
internal/providers   built-in Linear, GitHub, and Jira adapters
internal/linear      Linear GraphQL transport and collection browsing
internal/gitrepo     repository discovery and validation
internal/branch      ref discovery, template expansion, and branch planning
internal/worktree    worktree creation, reuse, metadata, and pruning
internal/tui         onboarding, pickers, progress, and terminal interaction
internal/doctor      environment diagnostics
internal/lwerr       typed user-facing errors and next actions
```

Read `SPEC.md` for behavioral contracts and `ARCHITECTURE.md` for package boundaries before
changing behavior. Keep those files, `README.md`, CLI help, and tests synchronized when a
user-visible contract changes.

## Verification

Run focused tests while developing. Before considering a change complete, run the repository
gate from `CONTRIBUTING.md`:

```sh
test -z "$(gofmt -l .)"
go test ./...
go test -race ./...
go vet ./...
go mod verify
shellcheck install.sh scripts/generate-third-party-notices.sh
./scripts/generate-third-party-notices.sh
git diff --check
git diff --exit-code -- THIRD_PARTY_NOTICES.md
```

Tests must not contact real issue providers, read real credentials, or modify user repositories. Worktree tests
should use temporary real Git repositories; other external behavior should use injected fakes.
