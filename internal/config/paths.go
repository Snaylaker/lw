package config

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// "windows" is accepted as a synonym for "win32" so runtime.GOOS can be handed
// straight through.
const (
	PlatformDarwin  = "darwin"
	PlatformWindows = "win32"
)

// HostPlatform names the running platform, using "win32" for Windows.
func HostPlatform() string {
	if runtime.GOOS == "windows" {
		return PlatformWindows
	}
	return runtime.GOOS
}

func isWindows(platform string) bool {
	return platform == PlatformWindows || platform == "windows"
}

// OSEnv snapshots the process environment. Every lookup below keys on
// *presence*, not emptiness, so an exported-but-empty variable is used as-is.
func OSEnv() map[string]string {
	entries := os.Environ()
	env := make(map[string]string, len(entries))
	for _, entry := range entries {
		if i := strings.IndexByte(entry, '='); i > 0 {
			env[entry[:i]] = entry[i+1:]
		}
	}
	return env
}

// HomeDir falls back to the OS home directory only when HOME is absent; an
// exported empty HOME is used verbatim.
func HomeDir(env map[string]string) string {
	if home, ok := env["HOME"]; ok {
		return home
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return home
}

// UserConfigDir is the platform-native location; LW_CONFIG_DIR overrides it.
// XDG is ignored on darwin and win32.
func UserConfigDir(env map[string]string, platform string) string {
	if platform == PlatformDarwin {
		return filepath.Join(HomeDir(env), "Library", "Application Support")
	}
	if isWindows(platform) {
		if dir, ok := env["APPDATA"]; ok {
			return dir
		}
		return filepath.Join(HomeDir(env), "AppData", "Roaming")
	}
	if dir, ok := env["XDG_CONFIG_HOME"]; ok {
		return dir
	}
	return filepath.Join(HomeDir(env), ".config")
}

// AppDirName is the tool's own directory inside the platform config directory,
// fixed by SPEC §7: ~/.config/lw, ~/Library/Application Support/lw, %APPDATA%\lw.
const AppDirName = "lw"

// Path is the config.json location. LW_CONFIG_DIR wins on presence
// alone, so an empty value yields the relative path "config.json".
func Path(env map[string]string, platform string) string {
	if dir, ok := env["LW_CONFIG_DIR"]; ok {
		return filepath.Join(dir, "config.json")
	}
	return filepath.Join(UserConfigDir(env, platform), AppDirName, "config.json")
}

// ExpandTilde treats only a leading "~" as a home reference; "~other/repo"
// names another user's home and is left untouched.
func ExpandTilde(path string, env map[string]string) string {
	if path == "~" {
		return HomeDir(env)
	}
	if strings.HasPrefix(path, "~/") {
		return filepath.Join(HomeDir(env), path[2:])
	}
	return path
}

// ResolveConfiguredPath makes a configured path value absolute.
func ResolveConfiguredPath(path string, env map[string]string) string {
	expanded := ExpandTilde(path, env)
	absolute, err := filepath.Abs(expanded)
	if err != nil {
		return filepath.Clean(expanded)
	}
	return absolute
}
