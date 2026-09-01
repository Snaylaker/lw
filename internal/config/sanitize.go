package config

import (
	"encoding/json"
	"sort"
	"strings"
)

// sanitizePathValue trims a configured path; a non-string or a blank one reads
// as absent, so the caller falls back to its default.
func sanitizePathValue(raw json.RawMessage) string {
	value, ok := AsString(raw)
	if !ok {
		return ""
	}
	return strings.TrimSpace(value)
}

// sanitizeCommandValue trims a configured shell line; a non-string or a blank
// one reads as absent. An invalid entry is dropped rather than fatal, so a
// mistyped credentialCommand costs the user a next action and not their whole
// configuration.
func sanitizeCommandValue(raw json.RawMessage) string {
	value, ok := AsString(raw)
	if !ok {
		return ""
	}
	return strings.TrimSpace(value)
}

func sanitizeProviderValue(raw json.RawMessage) string {
	value, ok := AsString(raw)
	if !ok {
		return ""
	}
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return ""
	}
	for _, character := range value {
		if character < 'a' || character > 'z' {
			if character < '0' || character > '9' {
				if character != '-' && character != '_' {
					return ""
				}
			}
		}
	}
	return value
}

// sanitizeBoolValue drops anything that is not a real boolean. For a flag whose
// only effect is deleting worktrees, an unparseable value must read as off.
func sanitizeBoolValue(raw json.RawMessage) bool {
	value, ok := AsBool(raw)
	return ok && value
}

// sanitizeRepoPreferences drops entries of the wrong shape rather than failing
// the file, as SPEC §7 requires of every section.
func sanitizeRepoPreferences(raw json.RawMessage) *RepoPreferences {
	record, ok := AsRecord(raw)
	if !ok {
		return nil
	}
	prefs := &RepoPreferences{unknown: unknownExcept(record, "roots", "recent", "projects", "teams")}
	if roots, ok := AsArray(record.Get("roots")); ok {
		for _, entry := range roots {
			if value := sanitizePathValue(entry); value != "" {
				prefs.Roots = append(prefs.Roots, value)
			}
		}
	}
	if recent, ok := AsArray(record.Get("recent")); ok {
		seen := map[string]bool{}
		for _, entry := range recent {
			item, ok := AsRecord(entry)
			if !ok {
				continue
			}
			path := sanitizePathValue(item.Get("path"))
			if path == "" || seen[path] {
				continue
			}
			usedAt, ok := AsNumber(item.Get("usedAt"))
			if !ok {
				continue
			}
			seen[path] = true
			prefs.Recent = append(prefs.Recent, RecentRepo{
				Path: path, UsedAt: int64(usedAt), unknown: unknownExcept(item, "path", "usedAt"),
			})
		}
		sort.SliceStable(prefs.Recent, func(i, j int) bool {
			return prefs.Recent[i].UsedAt > prefs.Recent[j].UsedAt
		})
		if len(prefs.Recent) > RecentPreferencesLimit {
			prefs.Recent = prefs.Recent[:RecentPreferencesLimit]
		}
	}
	prefs.Projects = sanitizeProjectRepos(record.Get("projects"))
	prefs.Teams = sanitizeTeamRepos(record.Get("teams"))
	if len(prefs.Roots) == 0 && len(prefs.Recent) == 0 && len(prefs.Projects) == 0 && len(prefs.Teams) == 0 && len(prefs.unknown) == 0 {
		return nil
	}
	return prefs
}

func sanitizeBranchNaming(raw json.RawMessage) *BranchNaming {
	record, ok := AsRecord(raw)
	if !ok {
		return nil
	}
	result := &BranchNaming{
		ByRepository: map[string]BranchRule{},
		unknown:      unknownExcept(record, "variables", "byRepository"),
	}
	if variables, ok := AsRecord(record.Get("variables")); ok {
		result.Variables.Username = sanitizeCommandValue(variables.Get("username"))
		result.Variables.unknown = unknownExcept(variables, "username")
	}
	if repositories, ok := AsRecord(record.Get("byRepository")); ok {
		for key, rawRule := range repositories {
			rule, ok := AsRecord(rawRule)
			if !ok {
				continue
			}
			name := strings.TrimSpace(key)
			template := sanitizeCommandValue(rule.Get("template"))
			if name != "" && template != "" {
				result.ByRepository[name] = BranchRule{
					Template: template, unknown: unknownExcept(rule, "template"),
				}
			}
		}
	}
	if result.Variables.Username == "" && len(result.Variables.unknown) == 0 && len(result.ByRepository) == 0 && len(result.unknown) == 0 {
		return nil
	}
	if len(result.ByRepository) == 0 {
		result.ByRepository = nil
	}
	return result
}

func sanitizePinPreferences(raw json.RawMessage) *PinPreferences {
	record, ok := AsRecord(raw)
	if !ok {
		return nil
	}
	pins := &PinPreferences{
		Projects: sanitizePinIDs(record.Get("projects")),
		Teams:    sanitizePinIDs(record.Get("teams")),
		unknown:  unknownExcept(record, "projects", "teams"),
	}
	if len(pins.Projects) == 0 && len(pins.Teams) == 0 && len(pins.unknown) == 0 {
		return nil
	}
	return pins
}

func sanitizePinIDs(raw json.RawMessage) []string {
	entries, ok := AsArray(raw)
	if !ok {
		return nil
	}
	seen := map[string]bool{}
	ids := make([]string, 0, len(entries))
	for _, entry := range entries {
		id, ok := AsString(entry)
		id = strings.TrimSpace(id)
		if !ok || id == "" || seen[id] {
			continue
		}
		seen[id] = true
		ids = append(ids, id)
		if len(ids) == PinPreferencesLimit {
			break
		}
	}
	if len(ids) == 0 {
		return nil
	}
	return ids
}

func sanitizeProjectRepos(raw json.RawMessage) []ProjectRepo {
	entries, ok := AsArray(raw)
	if !ok {
		return nil
	}
	byProject := map[string]ProjectRepo{}
	for _, entry := range entries {
		record, ok := AsRecord(entry)
		if !ok {
			continue
		}
		projectID, _ := AsString(record.Get("projectId"))
		projectID = strings.TrimSpace(projectID)
		path := sanitizePathValue(record.Get("path"))
		usedAt, hasUsedAt := AsNumber(record.Get("usedAt"))
		if projectID == "" || path == "" || !hasUsedAt {
			continue
		}
		candidate := ProjectRepo{
			ProjectID: projectID, Path: path, UsedAt: int64(usedAt),
			unknown: unknownExcept(record, "projectId", "path", "usedAt"),
		}
		if previous, seen := byProject[projectID]; !seen || candidate.UsedAt > previous.UsedAt {
			byProject[projectID] = candidate
		}
	}
	result := make([]ProjectRepo, 0, len(byProject))
	for _, association := range byProject {
		result = append(result, association)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].UsedAt == result[j].UsedAt {
			return result[i].ProjectID < result[j].ProjectID
		}
		return result[i].UsedAt > result[j].UsedAt
	})
	if len(result) > RecentPreferencesLimit {
		result = result[:RecentPreferencesLimit]
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

func sanitizeTeamRepos(raw json.RawMessage) []TeamRepo {
	entries, ok := AsArray(raw)
	if !ok {
		return nil
	}
	byTeam := map[string]TeamRepo{}
	for _, entry := range entries {
		record, ok := AsRecord(entry)
		if !ok {
			continue
		}
		teamID, _ := AsString(record.Get("teamId"))
		teamID = strings.TrimSpace(teamID)
		path := sanitizePathValue(record.Get("path"))
		usedAt, hasUsedAt := AsNumber(record.Get("usedAt"))
		if teamID == "" || path == "" || !hasUsedAt {
			continue
		}
		candidate := TeamRepo{
			TeamID: teamID, Path: path, UsedAt: int64(usedAt),
			unknown: unknownExcept(record, "teamId", "path", "usedAt"),
		}
		if previous, seen := byTeam[teamID]; !seen || candidate.UsedAt > previous.UsedAt {
			byTeam[teamID] = candidate
		}
	}
	result := make([]TeamRepo, 0, len(byTeam))
	for _, association := range byTeam {
		result = append(result, association)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].UsedAt == result[j].UsedAt {
			return result[i].TeamID < result[j].TeamID
		}
		return result[i].UsedAt > result[j].UsedAt
	})
	if len(result) > RecentPreferencesLimit {
		result = result[:RecentPreferencesLimit]
	}
	if len(result) == 0 {
		return nil
	}
	return result
}
