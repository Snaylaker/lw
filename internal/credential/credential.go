// Package credential resolves and persists the Read-only Linear API key.
// Advanced sources remain first: credentialCommand, then LINEAR_API_KEY. Normal
// onboarding saves to the system keychain, with explicit consent before a
// separate owner-only file on hosts such as headless Linux.
//
// The credential helper is the only user-configured child process. It receives
// the normal environment minus LINEAR_API_KEY, so configuring one credential
// source cannot leak the other into it.
package credential

import (
	"context"
	"errors"
	"io"
	"os/exec"
	"sort"
	"strings"
	"unicode"

	"github.com/snaylaker/lw/internal/config"
	"github.com/snaylaker/lw/internal/domain"
	"github.com/snaylaker/lw/internal/lwerr"
)

// EnvVar is the one environment variable lw reads a key from.
const EnvVar = "LINEAR_API_KEY"

// Source names where a resolved key came from, in the user's own vocabulary:
// each value is spelled exactly as the thing they would go and edit.
type Source string

const (
	SourceCommand Source = "credentialCommand"
	SourceEnv     Source = "LINEAR_API_KEY"
	SourceKeyring Source = "system keychain"
	SourceFile    Source = "owner-only credential file"
)

// Resolved is one key and the place it was found. The source is reported so
// `lw doctor` can say which source the run would actually use.
type Resolved struct {
	Credential domain.Credential
	Source     Source
}

// Runner executes credentialCommand through the platform shell and returns its
// standard output. Injected so no test ever runs a real credential helper.
type Runner func(ctx context.Context, shell string, args, environ []string) ([]byte, error)

// Options are the inputs of Resolve. Every zero value means "ask the host", so
// production callers pass the config and a test passes everything.
type Options struct {
	Env        map[string]string // nil means the process environment
	Platform   string            // empty means the host platform
	Command    string            // credentialCommand from config; empty means unset
	ConfigPath string            // config.json; also locates the fallback credential file
	Run        Runner            // nil means the real shell
	Vault      Vault             // nil means the system keychain plus owner-only file store
}

// Resolve reads the key. The order is credentialCommand, LINEAR_API_KEY, then
// onboarding's saved key. An explicitly configured command wins because a user
// who wrote one meant it.
//
// For the same reason a configured command that fails is an error rather than a
// silent fall-through to the environment — quietly using a different key than
// the one that was asked for is how a run ends up talking to the wrong
// workspace.
//
// No error message, from here or from the helper, ever carries the key: a
// credential helper's output *is* the secret, so it is never echoed, and
// neither is its standard error.
func Resolve(ctx context.Context, opts Options) (Resolved, error) {
	command := strings.TrimSpace(opts.Command)
	if command != "" {
		key, err := runCommand(ctx, command, opts)
		if err != nil {
			return Resolved{}, err
		}
		return Resolved{Credential: domain.Credential{Key: key}, Source: SourceCommand}, nil
	}

	env := opts.Env
	if env == nil {
		env = config.OSEnv()
	}
	if key := strings.TrimSpace(env[EnvVar]); key != "" {
		return Resolved{Credential: domain.Credential{Key: key}, Source: SourceEnv}, nil
	}

	vault := opts.Vault
	if vault == nil {
		vault = NewVault(configPath(opts))
	}
	key, source, err := vault.Load()
	if err == nil {
		key = strings.TrimSpace(key)
		if key != "" {
			return Resolved{Credential: domain.Credential{Key: key}, Source: source}, nil
		}
		return Resolved{}, missing(opts)
	}
	if !errors.Is(err, ErrNotFound) {
		return Resolved{}, lwerr.Wrap(err, lwerr.AuthRequired,
			"The saved Linear API key could not be read.",
			"fix the system keychain or credential file, then retry")
	}
	return Resolved{}, missing(opts)
}

// Save remembers a key entered during onboarding in exactly the store selected
// by the caller. Falling back from the keychain to a file is a UI decision that
// requires explicit consent, never an implicit persistence policy.
func Save(key string, target Store, opts Options) (Location, error) {
	key = strings.TrimSpace(key)
	if key == "" {
		return Location{}, missing(opts)
	}
	vault := opts.Vault
	if vault == nil {
		vault = NewVault(configPath(opts))
	}
	location, err := vault.Save(key, target)
	if errors.Is(err, ErrKeyringUnavailable) {
		return Location{}, lwerr.Wrap(err, lwerr.AuthRequired,
			"The system keychain is unavailable.",
			"approve the owner-only credential file, or cancel")
	}
	if err != nil {
		return Location{}, lwerr.Wrap(err, lwerr.AuthRequired,
			"The Linear API key could not be saved.",
			"fix permissions for the system keychain or config directory, then retry")
	}
	return location, nil
}

// Delete removes credentials owned by lw. Environment variables and a
// credentialCommand belong to the user and are deliberately untouched.
func Delete(opts Options) error {
	vault := opts.Vault
	if vault == nil {
		vault = NewVault(configPath(opts))
	}
	if err := vault.Delete(); err != nil {
		return lwerr.Wrap(err, lwerr.AuthRequired,
			"The saved Linear API key could not be removed.",
			"unlock or enable the system keychain, or fix credential file permissions, then retry")
	}
	return nil
}

// missing is the one case where no source answered. Both halves are
// literal: they are the whole of what a first-time user is told.
func missing(opts Options) *lwerr.Error {
	return lwerr.Wrap(ErrNotFound, lwerr.AuthRequired, "No Linear API key.",
		"connect inside lw, set LINEAR_API_KEY, or add credentialCommand to "+configPath(opts))
}

func configPath(opts Options) string {
	if opts.ConfigPath != "" {
		return opts.ConfigPath
	}
	env := opts.Env
	if env == nil {
		env = config.OSEnv()
	}
	return config.Path(env, opts.Platform)
}

// shellCommand is the platform shell and the flags that make it run one
// command line: `sh -c` everywhere but Windows, `cmd /c` there.
func shellCommand(platform string) (string, []string) {
	if platform == "" {
		platform = config.HostPlatform()
	}
	if platform == config.PlatformWindows || platform == "windows" {
		return "cmd", []string{"/c"}
	}
	return "sh", []string{"-c"}
}

// runCommand runs one credentialCommand line through the platform shell and
// returns the first line of its standard output.
func runCommand(ctx context.Context, command string, opts Options) (string, error) {
	run := opts.Run
	if run == nil {
		run = shellRunner
	}
	shell, shellArgs := shellCommand(opts.Platform)
	args := append(append([]string{}, shellArgs...), command)

	stdout, err := run(ctx, shell, args, commandEnvironment(opts.Env))
	if err != nil {
		// err may well quote the helper's own output. It is dropped here, and
		// deliberately not wrapped as a cause either, so nothing downstream can
		// print it.
		return "", lwerr.New(lwerr.AuthRequired,
			"credentialCommand failed.",
			"run it yourself to see why, or unset credentialCommand and set "+EnvVar+" instead")
	}
	key := firstLine(string(stdout))
	if key == "" {
		return "", lwerr.New(lwerr.AuthRequired,
			"credentialCommand printed no key.",
			"make it print the key on its first line, or unset credentialCommand and set "+EnvVar+" instead")
	}
	return key, nil
}

// firstLine is the key: everything before the first newline, with trailing
// whitespace trimmed so a helper that prints "key\r\n" or "key \n" still works.
// Leading whitespace is left alone — it is not this function's business to
// decide that a key cannot start with a space.
func firstLine(stdout string) string {
	line, _, _ := strings.Cut(stdout, "\n")
	return strings.TrimRightFunc(line, unicode.IsSpace)
}

// shellRunner is the real shell. Standard error is discarded rather than
// captured: exec.Cmd.Output would otherwise attach it to the returned error,
// and a credential helper's diagnostics can quote the secret it just read.
func shellRunner(ctx context.Context, shell string, args, environ []string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, shell, args...)
	cmd.Env = environ
	cmd.Stderr = io.Discard
	return cmd.Output()
}

// commandEnvironment preserves the caller's environment except for the API
// key. A configured helper wins over LINEAR_API_KEY, so it has no reason to
// inherit that unused secret.
func commandEnvironment(env map[string]string) []string {
	if env == nil {
		env = config.OSEnv()
	}
	keys := make([]string, 0, len(env))
	for key := range env {
		if !strings.EqualFold(key, EnvVar) {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	result := make([]string, 0, len(keys))
	for _, key := range keys {
		result = append(result, key+"="+env[key])
	}
	return result
}
