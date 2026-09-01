package cli

import (
	"context"

	"github.com/snaylaker/lw/internal/doctor"
)

// runDoctor is `lw doctor` (SPEC §11). doctor owns the report and the exit
// code; this is only the wiring, and it hands over the same seams the run uses
// so the report describes the run that would actually happen.
func runDoctor(ctx context.Context, opts Options, env *execEnv) int {
	extensions := make(map[string]string, len(env.providers))
	for id := range env.providers {
		extensions[string(id)] = providerDisplayName(id, env.providers)
	}
	return doctor.Run(ctx, doctor.Deps{
		Stdout:     env.stdout,
		Env:        env.env,
		Platform:   env.platform,
		Dir:        env.dir,
		Run:        env.run,
		ConfigPath: env.configPath(),
		Credential: env.credential,
		Vault:      env.vault,
		Extensions: extensions,
	})
}
