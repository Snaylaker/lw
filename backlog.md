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
