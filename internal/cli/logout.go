package cli

import (
	"context"
	"fmt"

	"github.com/snaylaker/lw/internal/credential"
)

// runLogout removes only the credential lw saved during onboarding. An
// environment variable or credentialCommand belongs to the user and is never
// edited behind their back.
func runLogout(_ context.Context, _ Options, env *execEnv) int {
	if err := credential.Delete(credential.Options{
		Env:        env.env,
		Platform:   env.platform,
		ConfigPath: env.configPath(),
		Vault:      env.vault,
	}); err != nil {
		return Report(err, env.stderr)
	}
	fmt.Fprintln(env.stdout, "Removed lw's saved Linear API key.")
	return 0
}
