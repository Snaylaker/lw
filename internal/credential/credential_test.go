package credential

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/snaylaker/lw/internal/lwerr"
)

// theKey is the fake secret every test uses. It is not a plausible Linear key
// and it never reaches a real process: the runner is always injected.
const theKey = "lin_api_TEST_NOT_A_REAL_KEY"

// recordingRunner stands in for the platform shell. It records the argv it was
// asked to run so a test can prove the command went through the shell rather
// than being exec'd directly.
type recordingRunner struct {
	stdout string
	err    error
	calls  int
	shell  string
	args   []string
	env    []string
}

func (r *recordingRunner) run(_ context.Context, shell string, args, environ []string) ([]byte, error) {
	r.calls++
	r.shell = shell
	r.args = append([]string(nil), args...)
	r.env = append([]string(nil), environ...)
	return []byte(r.stdout), r.err
}

// refuse is the runner for every test that must not touch a helper at all.
type emptyVault struct{}

func (emptyVault) Load() (string, Source, error) { return "", "", ErrNotFound }
func (emptyVault) Save(string, Store) (Location, error) {
	return KeyringLocation(), nil
}
func (emptyVault) Delete() error { return nil }

type memoryVault struct {
	key    string
	source Source
}

func (v *memoryVault) Load() (string, Source, error) { return v.key, v.source, nil }
func (v *memoryVault) Save(key string, target Store) (Location, error) {
	v.key = key
	if target == StoreFile {
		return FileLocation("/test/credentials"), nil
	}
	return KeyringLocation(), nil
}
func (v *memoryVault) Delete() error { v.key = ""; return nil }

func refuse(t *testing.T) Runner {
	t.Helper()
	return func(context.Context, string, []string, []string) ([]byte, error) {
		t.Fatal("credentialCommand must not run")
		return nil, nil
	}
}

func TestTheSourceNamesAreTheUsersOwnVocabulary(t *testing.T) {
	if EnvVar != "LINEAR_API_KEY" {
		t.Errorf("EnvVar = %q", EnvVar)
	}
	if SourceCommand != "credentialCommand" {
		t.Errorf("SourceCommand = %q", SourceCommand)
	}
	if SourceEnv != "LINEAR_API_KEY" {
		t.Errorf("SourceEnv = %q", SourceEnv)
	}
	if SourceKeyring != "system keychain" || SourceFile != "owner-only credential file" {
		t.Errorf("persistent sources = %q, %q", SourceKeyring, SourceFile)
	}
}

func TestCredentialCommandRunsThroughThePlatformShell(t *testing.T) {
	for _, tc := range []struct {
		platform  string
		wantShell string
		wantArgs  []string
	}{
		{"darwin", "sh", []string{"-c", "op read op://private/linear/api-key"}},
		{"linux", "sh", []string{"-c", "op read op://private/linear/api-key"}},
		{"windows", "cmd", []string{"/c", "op read op://private/linear/api-key"}},
		{"win32", "cmd", []string{"/c", "op read op://private/linear/api-key"}},
	} {
		t.Run(tc.platform, func(t *testing.T) {
			runner := &recordingRunner{stdout: theKey + "\n"}
			resolved, err := Resolve(context.Background(), Options{
				Env:      map[string]string{},
				Platform: tc.platform,
				Command:  "op read op://private/linear/api-key",
				Run:      runner.run,
			})
			if err != nil {
				t.Fatalf("Resolve: %v", err)
			}
			if resolved.Credential.Key != theKey {
				t.Errorf("key = %q", resolved.Credential.Key)
			}
			if resolved.Source != SourceCommand {
				t.Errorf("source = %q", resolved.Source)
			}
			if runner.shell != tc.wantShell || !reflect.DeepEqual(runner.args, tc.wantArgs) {
				t.Errorf("ran %q %q, want %q %q", runner.shell, runner.args, tc.wantShell, tc.wantArgs)
			}
		})
	}
}

// The key is the first line, trailing whitespace trimmed. A helper that prints
// a banner after the key, or a CRLF, or a trailing blank, must still work.
func TestTheKeyIsTheFirstLineOfStandardOutput(t *testing.T) {
	for _, tc := range []struct{ name, stdout, want string }{
		{"bare", theKey, theKey},
		{"trailing newline", theKey + "\n", theKey},
		{"crlf", theKey + "\r\n", theKey},
		{"trailing blanks", theKey + "  \t\n", theKey},
		{"later lines are ignored", theKey + "\nwarning: vault will expire\n", theKey},
		{"leading whitespace is kept", " " + theKey, " " + theKey},
	} {
		t.Run(tc.name, func(t *testing.T) {
			runner := &recordingRunner{stdout: tc.stdout}
			resolved, err := Resolve(context.Background(), Options{
				Env: map[string]string{}, Platform: "linux",
				Command: "helper", Run: runner.run,
			})
			if err != nil {
				t.Fatalf("Resolve: %v", err)
			}
			if resolved.Credential.Key != tc.want {
				t.Errorf("key = %q, want %q", resolved.Credential.Key, tc.want)
			}
		})
	}
}

func TestCredentialCommandWinsWithoutInheritingTheEnvironmentKey(t *testing.T) {
	runner := &recordingRunner{stdout: "from-the-command\n"}
	resolved, err := Resolve(context.Background(), Options{
		Env:      map[string]string{EnvVar: "from-the-environment", "PATH": "/tools"},
		Platform: "linux",
		Command:  "helper",
		Run:      runner.run,
	})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if resolved.Credential.Key != "from-the-command" || resolved.Source != SourceCommand {
		t.Fatalf("resolved = %+v", resolved)
	}
	if !reflect.DeepEqual(runner.env, []string{"PATH=/tools"}) {
		t.Fatalf("helper environment = %v, want PATH only", runner.env)
	}
}

func TestTheEnvironmentAnswersWhenNoCommandIsConfigured(t *testing.T) {
	for _, command := range []string{"", "   ", "\t\n"} {
		resolved, err := Resolve(context.Background(), Options{
			Env:      map[string]string{EnvVar: theKey},
			Platform: "linux",
			Command:  command,
			Run:      refuse(t),
		})
		if err != nil {
			t.Fatalf("Resolve with command %q: %v", command, err)
		}
		if resolved.Credential.Key != theKey || resolved.Source != SourceEnv {
			t.Fatalf("resolved = %+v", resolved)
		}
	}
}

func TestSavedCredentialIsUsedWhenCommandAndEnvironmentAreAbsent(t *testing.T) {
	vault := &memoryVault{key: theKey, source: SourceKeyring}
	resolved, err := Resolve(context.Background(), Options{Env: map[string]string{}, Vault: vault})
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Credential.Key != theKey || resolved.Source != SourceKeyring {
		t.Errorf("resolved = %+v", resolved)
	}
}

func TestTheEnvironmentValueIsTrimmed(t *testing.T) {
	resolved, err := Resolve(context.Background(), Options{
		Env: map[string]string{EnvVar: "  " + theKey + " \n"}, Platform: "linux",
	})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if resolved.Credential.Key != theKey {
		t.Errorf("key = %q", resolved.Credential.Key)
	}
}

// A variable set to whitespace is not a key: it must read as absent rather than
// authenticating with a blank string and getting a confusing 401 back.
func TestABlankEnvironmentValueIsAbsent(t *testing.T) {
	_, err := Resolve(context.Background(), Options{
		Env: map[string]string{EnvVar: "   "}, Platform: "linux", ConfigPath: "/c/config.json", Vault: emptyVault{},
	})
	if !lwerr.Is(err, lwerr.AuthRequired) {
		t.Fatalf("error = %v", err)
	}
}

func TestNeitherSourceIsAuthRequiredWithTheConfigPath(t *testing.T) {
	path := filepath.Join("/Users/me/.config/lw", "config.json")
	_, err := Resolve(context.Background(), Options{
		Env: map[string]string{}, Platform: "linux", ConfigPath: path, Run: refuse(t), Vault: emptyVault{},
	})
	e, ok := lwerr.As(err)
	if !ok {
		t.Fatalf("error = %v, want an lwerr", err)
	}
	if e.Kind != lwerr.AuthRequired {
		t.Errorf("kind = %q", e.Kind)
	}
	if e.Message != "No Linear API key." {
		t.Errorf("message = %q", e.Message)
	}
	if want := "connect inside lw, set LINEAR_API_KEY, or add credentialCommand to " + path; e.NextAction != want {
		t.Errorf("next action = %q, want %q", e.NextAction, want)
	}
}

// With no ConfigPath the message still has to name a real file, so the next
// action stays actionable when the caller did not bother to compute one.
func TestTheNextActionFallsBackToThePlatformConfigPath(t *testing.T) {
	_, err := Resolve(context.Background(), Options{
		Env:      map[string]string{"LW_CONFIG_DIR": "/opt/lw"},
		Platform: "linux",
		Vault:    emptyVault{},
	})
	e, _ := lwerr.As(err)
	if e == nil || e.NextAction != "connect inside lw, set LINEAR_API_KEY, or add credentialCommand to "+filepath.Join("/opt/lw", "config.json") {
		t.Fatalf("next action = %+v", e)
	}
}

// A configured command that fails is an error, not a fall-through: silently
// using a different key than the one that was asked for is how a run ends up in
// the wrong workspace.
func TestAFailingCredentialCommandDoesNotFallBackToTheEnvironment(t *testing.T) {
	runner := &recordingRunner{err: errors.New("exit status 1")}
	_, err := Resolve(context.Background(), Options{
		Env:      map[string]string{EnvVar: "from-the-environment"},
		Platform: "linux",
		Command:  "op read op://private/linear/api-key",
		Run:      runner.run,
	})
	e, ok := lwerr.As(err)
	if !ok {
		t.Fatalf("error = %v, want an lwerr", err)
	}
	if e.Kind != lwerr.AuthRequired {
		t.Errorf("kind = %q", e.Kind)
	}
	if e.Message != "credentialCommand failed." || strings.Contains(e.Message, "op read") {
		t.Errorf("message exposed command text: %q", e.Message)
	}
	if e.NextAction == "" {
		t.Error("no next action")
	}
}

func TestAnEmptyCredentialCommandOutputIsAnErrorWithoutCommandText(t *testing.T) {
	for _, stdout := range []string{"", "\n", "   \n" + theKey + "\n"} {
		runner := &recordingRunner{stdout: stdout}
		_, err := Resolve(context.Background(), Options{
			Env: map[string]string{EnvVar: theKey}, Platform: "linux",
			Command: "pass show linear", Run: runner.run,
		})
		e, ok := lwerr.As(err)
		if !ok {
			t.Fatalf("stdout %q: error = %v, want an lwerr", stdout, err)
		}
		if e.Kind != lwerr.AuthRequired || e.Message != "credentialCommand printed no key." {
			t.Fatalf("stdout %q: error = %+v", stdout, e)
		}
	}
}

// The whole point of the helper is that its output is the secret. Neither the
// output nor the failure's own text may survive into anything a user or a log
// could see.
func TestACredentialCommandFailureNeverEchoesItsOutputOrItsStderr(t *testing.T) {
	const leaked = "SECRET-lin_api_leaked_through_stderr"
	runner := &recordingRunner{stdout: leaked, err: errors.New("helper wrote: " + leaked)}
	_, err := Resolve(context.Background(), Options{
		Env: map[string]string{}, Platform: "linux", ConfigPath: "/c/config.json",
		Command: "helper", Run: runner.run,
	})
	if err == nil {
		t.Fatal("want an error")
	}
	// Error() walks the cause chain, so this covers a wrapped cause too.
	if strings.Contains(err.Error(), leaked) {
		t.Fatalf("the failure echoed the helper: %q", err.Error())
	}
	e, _ := lwerr.As(err)
	if e == nil || strings.Contains(e.Message+e.NextAction, leaked) {
		t.Fatalf("the failure echoed the helper: %+v", e)
	}
	if e.Cause != nil && strings.Contains(e.Cause.Error(), leaked) {
		t.Fatalf("the cause echoed the helper: %v", e.Cause)
	}
}

// A successful resolution says nothing about the key either: only the source
// name is safe to print, and `lw doctor` prints exactly that.
func TestAResolvedSourceIsSafeToPrint(t *testing.T) {
	runner := &recordingRunner{stdout: theKey}
	resolved, _ := Resolve(context.Background(), Options{
		Env: map[string]string{}, Platform: "linux", Command: "helper", Run: runner.run,
	})
	if strings.Contains(string(resolved.Source), theKey) {
		t.Fatalf("source = %q", resolved.Source)
	}
}
