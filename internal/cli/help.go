package cli

// Version is the build stamp printed by --version. Releases override it with
//
//	go build -ldflags "-X github.com/snaylaker/lw/internal/cli.Version=v0.1.0"
//
// so a source build is honestly labelled "dev".
var Version = "dev"

// helpText lists every command and every flag. It is printed by --help, and
// again after the message of any usage error, so a wrong invocation always ends
// looking at the right one.
const helpText = `lw — pick a Linear issue, get a git worktree for it.

lw prints the worktree path on stdout and nothing else, so it composes:

  cd "$(lw)"                      search issues, then change into the worktree
  cd "$(lw --issue ENG-3971)"     no UI at all: straight to the worktree
  lw --repo ~/src/api             search issues for this repository

Everything else — the pickers, progress, warnings, errors — goes to stderr.
What you do in the worktree is up to you: lw starts no editor, shell or agent.

Usage:
  lw [flags]                      interactive issue search, repository, then the path
  lw doctor                       check the environment
  lw context [--json]             print this worktree's ticket context
  lw summary <text>               record what the work is now about
  lw prune [--yes] [--no-fetch]   remove worktrees whose branch is merged or gone
  lw logout                       remove the key saved during onboarding

In interactive search, Tab cycles issue/project/team views; Ctrl+P pins a project or team.

Run flags:
  --repo <path>           use this repository and skip repository selection
  --issue <IDENT>         resolve one issue directly — no terminal UI is used

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
