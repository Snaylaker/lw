package gitrepo

import (
	"context"
	"runtime"
	"testing"
)

func TestDefaultRunnerDoesNotPassTheLinearKeyToChildren(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the release workflow exercises this helper on Unix")
	}
	t.Setenv("LINEAR_API_KEY", "must-not-escape")
	result, err := DefaultRunner(context.Background(), t.TempDir(), "sh", []string{"-c", `printf %s "${LINEAR_API_KEY-unset}"`})
	if err != nil {
		t.Fatal(err)
	}
	if result.ExitCode != 0 || result.Stdout != "unset" {
		t.Errorf("child saw key: result = %+v", result)
	}
}
