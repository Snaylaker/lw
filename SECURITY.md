# Security policy

## Supported versions

Security fixes are provided for the latest released version of `lw`.

## Report a vulnerability

Please use [GitHub private vulnerability reporting](https://github.com/snaylaker/lw/security/advisories/new).
Do not open a public issue for a suspected vulnerability.

Never include a provider API token, credential-command output, private repository path, or real
issue data in a report. Replace sensitive values with synthetic examples and describe their
shape instead.

Include the affected version, operating system, reproduction steps, and expected impact.
You should receive an acknowledgment within seven days.

## Security boundaries

`lw` sends read-only requests to the selected Linear, GitHub, or Jira endpoint. Custom GitHub
and Jira URLs must be HTTPS except for localhost development. It has no telemetry or hosted
service. It stores local preferences and worktree metadata, and it stores only the Linear key,
inside the operating-system credential store or an explicitly approved owner-only file. GitHub
and Jira tokens remain environment-owned. Custom providers declare sensitive environment
variables through `provider.SensitiveEnvironmentProvider`; `lw` removes all declared provider
secrets from Git children and commands launched through `lw run`. A configured
`credentialCommand` is intentionally executed through the platform shell and must be treated as
trusted local configuration.
