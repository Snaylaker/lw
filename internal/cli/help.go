package cli

// Version is the build stamp printed by --version. Releases override it with
//
//	go build -ldflags "-X github.com/snaylaker/lw/internal/cli.Version=v0.2.0"
//
// so a source build is honestly labelled "dev".
var Version = "dev"

// helpText lists every command and every flag. It is printed by --help, and
// again after the message of any usage error, so a wrong invocation always ends
// looking at the right one.
const helpText = `lw — pick a Linear issue and prepare a git worktree.

Plain lw prints the worktree path on stdout and nothing else:

  cd "$(lw)"                      search issues, then change into the worktree
  cd "$(lw --issue ENG-3971 --branch alex/eng-3971-fix)"
                                      resolve one issue without the terminal UI
  lw --repo ~/src/api             search issues for this repository

Use lw run to start an explicit command after creating or reusing the worktree:

  lw run -- claude
  lw run -- codex --full-auto
  lw run -- cursor .

The command runs directly, without a shell, in the worktree. It owns stdin,
stdout, stderr and its exit code. LINEAR_API_KEY is removed from its environment.

Usage:
  lw [flags]                              issue, repository, then path on stdout
  lw run [flags] -- <command> [args...]   issue, repository, then run the command
  lw doctor                               check the environment
  lw branches set-rule [--username <name>] <template>
  lw branches show-rule
  lw branches preview <IDENT>
  lw branches unset-rule
  lw context [--json]                     print this worktree's ticket context
  lw summary <text>                       record what the work is now about
  lw prune [--yes] [--no-fetch]           remove merged or gone worktrees
  lw logout                               remove the key saved during onboarding

In interactive search, Tab cycles issue/project/team views; Ctrl+P pins a project or team.

Repository and flow flags:
  --repo <path>           use this repository; valid for flows and lw branches
  --issue <IDENT>         resolve one issue directly; no terminal UI is used
  --branch <name>         use this branch (required for a new branch in direct mode
                          unless this repository has a branchNaming template)

lw branches:
  set-rule <template>     save this repository's branch naming template
  show-rule               show this repository's rule and username
  preview <IDENT>         expand and Git-validate the rule for a Linear issue
  unset-rule              remove this repository's rule
  --username <name>       set the shared explicit {username} template value

Templates support {username}, {ticket}, {ticket_lower}, {slug}, and {linear_branch}.

lw prune flags:
  --yes                   actually remove; without it prune only reports
  --no-fetch              skip the prune-fetch and judge from local refs only
  --auto / --no-auto      save the preference: prune automatically on every run

Other flags:
  --json                  lw context: print the metadata object verbatim
  --version               print the version and exit
  --help                  print this help and exit

Credentials:
  On first run, paste a Read-only Linear API key inside lw. It is saved in the
  system keychain when available; owner-only file storage requires approval.
  credentialCommand and LINEAR_API_KEY remain available for advanced use.

Exit codes: 0 success · 1 error · 2 usage · 130 cancelled.
`

// HelpText is the usage text, ending in a newline.
func HelpText() string { return helpText }
