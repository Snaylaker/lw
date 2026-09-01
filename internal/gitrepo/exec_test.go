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

func TestNewRunnerRemovesCustomProviderSecretsFromGitChildren(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell probe is Unix-only")
	}
	t.Setenv("TICKETS_TOKEN", "secret")
	result, err := NewRunner([]string{"TICKETS_TOKEN"})(
		context.Background(), t.TempDir(), "sh", []string{"-c", `printf %s "$TICKETS_TOKEN"`},
	)
	if err != nil || result.ExitCode != 0 {
		t.Fatalf("runner = %+v, %v", result, err)
	}
	if result.Stdout != "" {
		t.Fatalf("custom provider secret reached child: %q", result.Stdout)
	}
}
