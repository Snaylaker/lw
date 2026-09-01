# Backlog

## Resolve ticket branches before creating one

Status: implemented.

`lw` previously reused only an exact local branch named after the issue identifier. If that branch does not exist, it silently creates one, even when a matching remote branch already carries the work.

Implemented behavior:

- Search local and fetched remote branches for the ticket identifier before creating anything.
- Reuse the branch when there is one unambiguous match.
- Ask the user when several branches match.
- When no branch exists, let the user choose a convention-compliant name instead of silently using the issue identifier.
- Prefill the prompt with Linear's suggested branch name, but keep it editable because it may differ from the actual repository convention.
- In non-interactive mode, require an explicit branch name or a configured branch-name template before creating a branch.
- Create new branches from the latest remote default branch rather than the source checkout's current `HEAD`.

## Manage repository branch rules from the CLI

Status: implemented.

- Save a repository rule with `lw branches set-rule`.
- Inspect it with `lw branches show-rule`.
- Expand and Git-validate it against a real issue with `lw branches preview`.
- Remove it with `lw branches unset-rule`.

## Read issues from multiple providers

Status: implemented.

- Expose the compile-time `provider.Provider` and neutral `provider.WorkItem` contracts.
- Preserve Linear as the default with its existing onboarding and collection browsers.
- Resolve and search read-only GitHub issues through REST, excluding pull requests.
- Resolve and search read-only Jira Cloud issues through REST and enhanced JQL.
- Keep provider references separate from filesystem-safe worktree identities.
- Route all providers through the same branch discovery, templates, worktree, metadata, and
  command-launch behavior.

Runtime provider discovery remains out of scope; custom providers require a custom build.
