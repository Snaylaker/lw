package cli

import (
	"context"

	"github.com/snaylaker/lw/internal/doctor"
)

// runDoctor is `lw doctor` (SPEC §11). doctor owns the report and the exit
// code; this is only the wiring, and it hands over the same seams the run uses
// so the report describes the run that would actually happen.
func runDoctor(ctx context.Context, opts Options, env *execEnv) int {
	return doctor.Run(ctx, doctor.Deps{
		Stdout:     env.stdout,
		Env:        env.env,
		Platform:   env.platform,
		Dir:        env.dir,
		Run:        env.run,
		ConfigPath: env.configPath(),
		Credential: env.credential,
		Vault:      env.vault,
	})
}
