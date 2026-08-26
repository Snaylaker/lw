package config

import (
	"path/filepath"
	"testing"
)

// The win32 rows are asserted with the host separator, exactly as the
// The platform argument picks the layout, path/filepath
// picks the separator.
//
// SPEC §7 fixes the directory the config file lives in: `~/.config/lw` on
// Linux, `~/Library/Application Support/lw` on macOS, `%APPDATA%\lw` on
// Windows. It is user-visible — the missing-credential next action, the
// malformed-config error and `lw doctor` all print it.
func TestPlatformPaths(t *testing.T) {
	home := map[string]string{"HOME": "/home/u"}
	xdg := map[string]string{"HOME": "/home/u", "XDG_CONFIG_HOME": "/xdg/cfg"}
	windows := map[string]string{"HOME": "/home/u", "APPDATA": "/windows/roaming"}
	overridden := map[string]string{
		"HOME":                      "/home/u",
		"LW_CONFIG_DIR":             "/opt/lw/config",
		"NOT_A_CONFIG_DIRECTORY":    "ignored",
		"XDG_CONFIG_HOME_LOOKALIKE": "ignored",
	}

	cases := []struct {
		name string
		got  string
		want string
	}{
		{"darwin config", Path(home, "darwin"), "/home/u/Library/Application Support/lw/config.json"},
		{"linux config", Path(home, "linux"), "/home/u/.config/lw/config.json"},
		{"linux xdg config", UserConfigDir(xdg, "linux"), "/xdg/cfg"},
		{"darwin ignores xdg config", UserConfigDir(xdg, "darwin"), "/home/u/Library/Application Support"},
		{"win32 config dir", UserConfigDir(windows, "win32"), "/windows/roaming"},
		{"win32 config", Path(windows, "win32"), filepath.Join("/windows/roaming", "lw", "config.json")},
		{"overridden config dir", Path(overridden, "linux"), "/opt/lw/config/config.json"},
	}
	for _, tc := range cases {
		if tc.got != tc.want {
			t.Errorf("%s = %q, want %q", tc.name, tc.got, tc.want)
		}
	}
}

func TestWindowsFallbacksWithoutAppData(t *testing.T) {
	env := map[string]string{"HOME": "/home/u"}
	if got, want := UserConfigDir(env, "win32"), filepath.Join("/home/u", "AppData", "Roaming"); got != want {
		t.Errorf("UserConfigDir = %q, want %q", got, want)
	}
	// The runtime.GOOS spelling must select the same layout as "win32" does.
	if UserConfigDir(env, "windows") != UserConfigDir(env, "win32") {
		t.Error(`"windows" must be a synonym for "win32"`)
	}
}

// An exported-but-empty variable is used as written: the lookup coalesces
// on absence only.
func TestEmptyEnvValuesAreUsedVerbatim(t *testing.T) {
	if got := Path(map[string]string{"LW_CONFIG_DIR": ""}, "linux"); got != "config.json" {
		t.Errorf("Path = %q, want %q", got, "config.json")
	}
	if got := UserConfigDir(map[string]string{"HOME": "/h", "XDG_CONFIG_HOME": ""}, "linux"); got != "" {
		t.Errorf("UserConfigDir = %q, want %q", got, "")
	}
	if got := HomeDir(map[string]string{"HOME": ""}); got != "" {
		t.Errorf("HomeDir = %q, want %q", got, "")
	}
}

func TestExpandTilde(t *testing.T) {
	home := map[string]string{"HOME": "/tmp/fake-home"}
	cases := []struct{ got, want string }{
		{ExpandTilde("~", home), "/tmp/fake-home"},
		{ExpandTilde("~/Work/repo", home), "/tmp/fake-home/Work/repo"},
		{ExpandTilde("/abs/repo", home), "/abs/repo"},
		{ExpandTilde("~other/repo", home), "~other/repo"},
	}
	for i, tc := range cases {
		if tc.got != tc.want {
			t.Errorf("case %d = %q, want %q", i, tc.got, tc.want)
		}
	}
}
