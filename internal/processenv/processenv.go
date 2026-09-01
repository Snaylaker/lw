// Package processenv builds child-process environments without leaking
// provider credentials.
package processenv

import (
	"sort"
	"strings"
)

var builtInProviderSecrets = []string{
	"LINEAR_API_KEY",
	"GITHUB_TOKEN",
	"GH_TOKEN",
	"JIRA_API_TOKEN",
}

// BuiltInProviderSecrets returns the credential variables used by the official
// providers. Callers may append without mutating the process-wide policy.
func BuiltInProviderSecrets() []string {
	return append([]string(nil), builtInProviderSecrets...)
}

// Without removes sensitive variable names case-insensitively while preserving
// the order and spelling of every other entry.
func Without(environ, sensitive []string) []string {
	blocked := nameSet(sensitive)
	result := make([]string, 0, len(environ))
	for _, entry := range environ {
		name, _, _ := strings.Cut(entry, "=")
		if _, found := blocked[strings.ToLower(name)]; !found {
			result = append(result, entry)
		}
	}
	return result
}

// FromMap renders a deterministic environment after removing sensitive names.
func FromMap(env map[string]string, sensitive []string) []string {
	blocked := nameSet(sensitive)
	keys := make([]string, 0, len(env))
	for key := range env {
		if _, found := blocked[strings.ToLower(key)]; !found {
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

func nameSet(names []string) map[string]struct{} {
	result := make(map[string]struct{}, len(names))
	for _, name := range names {
		name = strings.ToLower(strings.TrimSpace(name))
		if name != "" && !strings.Contains(name, "=") {
			result[name] = struct{}{}
		}
	}
	return result
}
