# Security policy

## Supported versions

Security fixes are provided for the latest released version of `lw`.

## Report a vulnerability

Please use [GitHub private vulnerability reporting](https://github.com/snaylaker/lw/security/advisories/new).
Do not open a public issue for a suspected vulnerability.

Never include a Linear API key, credential-command output, private repository path, or real
issue data in a report. Replace sensitive values with synthetic examples and describe their
shape instead.

Include the affected version, operating system, reproduction steps, and expected impact.
You should receive an acknowledgment within seven days.

## Security boundaries

`lw` sends GraphQL requests only to Linear's documented API endpoint. It has no telemetry
or hosted service. It stores local preferences and worktree metadata, and it stores a Linear
key only in the operating-system credential store or an explicitly approved owner-only file.
A configured `credentialCommand` is intentionally executed through the platform shell and
must be treated as trusted local configuration.
