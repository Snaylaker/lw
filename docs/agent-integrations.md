# Coding agent integrations

`lw context` gives a coding agent the issue attached to its current worktree without making a
Linear request. Outside an `lw` worktree it prints nothing and exits successfully, so the same
setup can run in every repository.

```sh
lw context
lw context --json
```

The plain form is best for prompts and hooks. The JSON form is best for scripts. Context can
contain an issue title, URL, team key, and local summary, so review your model provider's data
policy before injecting it into a hosted agent.

## Shared instruction

Agents without a context-injection hook can use this instruction:

```markdown
## lw worktree context

- At the beginning of each session, run `lw context`.
- If it prints nothing, continue normally.
- If it prints a ticket, use it as read-only task context. It does not expand the user's request.
- Never print, inspect, or pass through `LINEAR_API_KEY`.
- Do not run `lw summary` unless the user asks you to update the local summary.
```

## Claude Code

Claude Code can inject plain stdout from a `SessionStart` hook. Merge this into either
`~/.claude/settings.json` for all local projects or `.claude/settings.json` for one project:

```json
{
  "hooks": {
    "SessionStart": [
      {
        "hooks": [
          {
            "type": "command",
            "command": "cd \"$CLAUDE_PROJECT_DIR\" && lw context"
          }
        ]
      }
    ]
  }
}
```

The hook is safe outside an `lw` worktree because it produces no output. Existing settings and
hooks should be merged, not overwritten.

Reference: [Claude Code hooks](https://code.claude.com/docs/en/hooks)

## Cursor

Cursor's `sessionStart` hook expects JSON. This example uses `jq` to wrap `lw context` in
Cursor's `additional_context` field. Merge it into `~/.cursor/hooks.json`:

```json
{
  "version": 1,
  "hooks": {
    "sessionStart": [
      {
        "type": "command",
        "command": "cd \"$CURSOR_PROJECT_DIR\" && lw context | jq -Rs '{\"additional_context\": .}'"
      }
    ]
  }
}
```

Project hooks can instead live in `.cursor/hooks.json`. Local `sessionStart` hooks do not run
for Cursor cloud agents. A cloud checkout also lacks the private Git metadata created on your
machine, so pass reviewed context explicitly when using a cloud agent.

Reference: [Cursor hooks](https://cursor.com/docs/hooks)

## OpenAI Codex

Codex reads `AGENTS.md` before starting work. Append the [shared instruction](#shared-instruction)
to `~/.codex/AGENTS.md` for all repositories or to a repository's root `AGENTS.md` for a shared
project rule.

Codex has no documented session-start command hook equivalent to Claude Code's. The instruction
therefore asks Codex to run `lw context` as its first command rather than injecting command output
before the session starts.

Reference: [Codex custom instructions with AGENTS.md](https://developers.openai.com/codex/guides/agents-md)

## Gemini CLI

Gemini CLI supports `SessionStart` context injection. Its hook protocol requires JSON, so this
example uses `jq`. Merge it into `~/.gemini/settings.json` or `.gemini/settings.json`:

```json
{
  "hooks": {
    "SessionStart": [
      {
        "matcher": "*",
        "hooks": [
          {
            "name": "lw-context",
            "type": "command",
            "command": "cd \"$GEMINI_PROJECT_DIR\" && lw context | jq -Rs '{\"hookSpecificOutput\":{\"additionalContext\":.}}'"
          }
        ]
      }
    ]
  }
}
```

Reference: [Gemini CLI hooks](https://geminicli.com/docs/hooks/)

## GitHub Copilot

For local Copilot agent sessions in a checked-out `lw` worktree, append the
[shared instruction](#shared-instruction) to `.github/copilot-instructions.md`.

GitHub-hosted coding agents clone the remote repository and cannot access the local worktree's
private `lw.json`. Do not commit `lw.json` to bridge that gap. Instead, provide a reviewed issue
summary in the task you send to the hosted agent.

Reference: [GitHub Copilot repository instructions](https://docs.github.com/en/copilot/how-tos/copilot-on-github/customize-copilot/add-custom-instructions/add-repository-instructions)

## Windsurf and Devin

Windsurf and Devin discover a root `AGENTS.md` as an always-on workspace rule. Append the
[shared instruction](#shared-instruction) there. This also works alongside Codex when both tools
use the same repository.

For a user-wide Cascade rule, place the same text in
`~/.codeium/windsurf/memories/global_rules.md`. Repository instructions are preferable when a
team should review and share the behavior.

References:

- [Windsurf and Devin AGENTS.md](https://docs.windsurf.com/windsurf/cascade/agents-md)
- [Windsurf rules](https://docs.windsurf.com/windsurf/cascade/memories)

## Other agents

For any local agent that can run shell commands, add the [shared instruction](#shared-instruction)
to its global or repository instruction file. If it has a session-start hook:

1. Run the hook from the repository or worktree directory.
2. Execute `lw context`.
3. Inject non-empty stdout as context.
4. Keep the hook silent when stdout is empty.

Use `lw context --json` when the hook needs structured fields. The JSON object contains exactly
`identifier`, `title`, `url`, `team`, and `summary`.

## Updating the local summary

If work moves away from the issue title, the user can record a local clarification:

```sh
lw summary "the failure is in repository discovery, not worktree creation"
```

Later sessions receive that summary through `lw context`. This updates only private local Git
metadata and never writes to Linear.
