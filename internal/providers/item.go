// Package providers validates results returned through the public provider contract.
package providers

import (
	"fmt"
	"strings"
	"unicode/utf8"

	issueprovider "github.com/snaylaker/lw/provider"
)

// Normalize is the single trust boundary for provider output. The selected
// provider owns identity; a WorkItem cannot impersonate another provider or
// supply a path-unsafe worktree key.
func Normalize(source issueprovider.ID, item issueprovider.WorkItem) (issueprovider.WorkItem, error) {
	if item.Provider == "" {
		item.Provider = source
	}
	if item.Provider != source {
		return issueprovider.WorkItem{}, invalidItem(source, "returned provider %q", item.Provider)
	}
	worktreeKey := strings.TrimSpace(item.WorktreeKey)
	if worktreeKey != item.WorktreeKey {
		return issueprovider.WorkItem{}, invalidItem(source, "returned worktree key %q with surrounding whitespace", item.WorktreeKey)
	}
	if !portablePathSegment(item.WorktreeKey) {
		return issueprovider.WorkItem{}, invalidItem(source, "returned unsafe worktree key %q", item.WorktreeKey)
	}
	item.Reference = strings.TrimSpace(item.Reference)
	if item.Reference == "" {
		item.Reference = item.WorktreeKey
	}

	item.ID = strings.TrimSpace(item.ExternalID)
	item.Provider = source
	item.ExternalID = strings.TrimSpace(item.ExternalID)
	item.Title = strings.TrimSpace(item.Title)
	item.URL = strings.TrimSpace(item.URL)
	item.StateType = strings.TrimSpace(item.StateType)
	item.StateName = strings.TrimSpace(item.StateName)
	item.SuggestedBranch = strings.TrimSpace(item.SuggestedBranch)
	item.BranchKeys = cleanBranchKeys(item.BranchKeys)
	if item.ID == "" {
		item.ID = string(source) + ":" + item.Reference
	}
	scopes := item.Scopes
	item.Scopes = item.Scopes[:0]
	for _, scope := range scopes {
		scope.Kind = strings.TrimSpace(scope.Kind)
		scope.ID = strings.TrimSpace(scope.ID)
		scope.Key = strings.TrimSpace(scope.Key)
		scope.Name = strings.TrimSpace(scope.Name)
		if scope.Kind != "" && scope.ID != "" {
			item.Scopes = append(item.Scopes, scope)
		}
	}
	return item, nil
}

func NormalizeAll(source issueprovider.ID, items []issueprovider.WorkItem) ([]issueprovider.WorkItem, error) {
	result := make([]issueprovider.WorkItem, 0, len(items))
	for _, item := range items {
		issue, err := Normalize(source, item)
		if err != nil {
			return nil, err
		}
		result = append(result, issue)
	}
	return result, nil
}

func cleanBranchKeys(keys []string) []string {
	seen := map[string]bool{}
	result := make([]string, 0, len(keys))
	for _, key := range keys {
		key = strings.TrimSpace(key)
		if key != "" && !seen[key] {
			seen[key] = true
			result = append(result, key)
		}
	}
	return result
}

func portablePathSegment(value string) bool {
	if value == "" || value == "." || value == ".." || !utf8.ValidString(value) || len(value) > 255 ||
		strings.ContainsAny(value, `/\\<>:"|?*`) || strings.HasSuffix(value, ".") || strings.HasSuffix(value, " ") {
		return false
	}
	for _, character := range value {
		if character < 0x20 || character == 0x7f {
			return false
		}
	}
	base := strings.ToUpper(strings.TrimSuffix(value, filepathExtension(value)))
	switch base {
	case "CON", "PRN", "AUX", "NUL",
		"COM1", "COM2", "COM3", "COM4", "COM5", "COM6", "COM7", "COM8", "COM9",
		"LPT1", "LPT2", "LPT3", "LPT4", "LPT5", "LPT6", "LPT7", "LPT8", "LPT9":
		return false
	}
	return true
}

func filepathExtension(value string) string {
	if index := strings.IndexByte(value, '.'); index >= 0 {
		return value[index:]
	}
	return ""
}

func invalidItem(source issueprovider.ID, format string, args ...any) error {
	return fmt.Errorf("provider %q %s", source, fmt.Sprintf(format, args...))
}
