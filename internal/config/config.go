// Package config reads and writes the non-secret preference file. Provider
// API tokens are never stored in config.json: the file may name a Linear
// `credentialCommand` to fetch it, while onboarding stores the key separately
// in the system credential store or an explicitly approved owner-only file.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"sort"
	"strings"
	"syscall"

	"github.com/snaylaker/lw/internal/lwerr"
)

// RecentRepo is a repository the user picked before. Newest first.
type RecentRepo struct {
	Path   string `json:"path"`
	UsedAt int64  `json:"usedAt"`
}

// ProjectRepo remembers the repository selected for a Linear project. It is a
// durable local preference rather than data derived from a search result.
type ProjectRepo struct {
	ProjectID string `json:"projectId"`
	Path      string `json:"path"`
	UsedAt    int64  `json:"usedAt"`
}

// TeamRepo is the fallback for a projectless issue. Project associations stay
// more specific; a team association is consulted only when no project exists.
type TeamRepo struct {
	TeamID string `json:"teamId"`
	Path   string `json:"path"`
	UsedAt int64  `json:"usedAt"`
}

// RepoUse is one issue/repository choice recorded atomically.
type RepoUse struct {
	ProjectID string
	TeamID    string
	Path      string
}

// RepoPreferences is where the repo picker looks. Roots are scanned one level
// deep; Recent orders the picker, Projects route project issues, and Teams route
// projectless issues.
type RepoPreferences struct {
	Roots    []string      `json:"roots,omitempty"`
	Recent   []RecentRepo  `json:"recent,omitempty"`
	Projects []ProjectRepo `json:"projects,omitempty"`
	Teams    []TeamRepo    `json:"teams,omitempty"`
}

// PinPreferences keeps stable Linear IDs, not list responses. Pins therefore
// survive refreshes without becoming a cache of workspace data.
type PinPreferences struct {
	Projects []string `json:"projects,omitempty"`
	Teams    []string `json:"teams,omitempty"`
}

// PinToggle is the complete pin state after one toggle.
type PinToggle struct {
	Pinned bool
	IDs    []string
}

// BranchRule is one repository's safe branch-name template. Templates are
// expanded by lw; they are data, never shell commands.
type BranchRule struct {
	Template string `json:"template"`
}

// BranchVariables holds explicit values shared by branch templates. Username
// is configured rather than guessed from git's human-readable user.name.
type BranchVariables struct {
	Username string `json:"username,omitempty"`
}

// BranchNaming maps a stable remote repository key (for example
// gitlab.example.com/group/repo) or an absolute checkout path to its rule.
type BranchNaming struct {
	Variables    BranchVariables       `json:"variables,omitempty"`
	ByRepository map[string]BranchRule `json:"byRepository,omitempty"`
}

// BranchRuleUpdate is one atomic repository-rule change. A blank Username
// preserves the existing global template variable.
type BranchRuleUpdate struct {
	Repository string
	Template   string
	Username   string
}

// StoredConfig is the whole file. Every section is optional, and unknown keys
// are ignored on read and dropped on the next write. No secret is ever stored
// here: CredentialCommand names a way to *fetch* the key, and the key itself
// never comes back to this file.
type StoredConfig struct {
	Repos        *RepoPreferences `json:"repos,omitempty"`
	Pins         *PinPreferences  `json:"pins,omitempty"`
	BranchNaming *BranchNaming    `json:"branchNaming,omitempty"`
	// IssueProvider selects the default read-only issue service. Empty preserves
	// the original Linear behavior.
	IssueProvider string `json:"issueProvider,omitempty"`
	// WorktreeRoot holds the checkouts, one subdirectory per repository. Empty
	// means DefaultWorktreeRoot. Absolute or "~"-prefixed, so the file survives
	// a home move.
	WorktreeRoot string `json:"worktreeRoot,omitempty"`
	// CredentialCommand is a shell line that prints the Linear API key on its
	// first line, e.g. "op read op://private/linear/api-key". Empty means the
	// key comes from LINEAR_API_KEY instead. It is a reference, never a key:
	// writing the key here is exactly what lw refuses to do.
	CredentialCommand string `json:"credentialCommand,omitempty"`
	// PruneMerged opts in to removing finished worktrees automatically at the
	// selected repository before opening its worktree. Off by default: deletion is not
	// a default anyone should discover by surprise.
	PruneMerged bool `json:"pruneMerged,omitempty"`
}

// DefaultWorktreeRoot is where checkouts go when the config names no other
// place.
const DefaultWorktreeRoot = "~/.lw/worktrees"

// RecentPreferencesLimit keeps a long-lived (or hand-edited) recents list from
// growing without bound.
const RecentPreferencesLimit = 20

// PinPreferencesLimit prevents a hand-edited pin list from growing without
// bound while leaving ample room for real project and team favorites.
const PinPreferencesLimit = 100

// The literal pair of SPEC §7. This file never holds the key: onboarding uses
// the system keychain or a separate credential file, and credentialCommand is
// only a reference. Deleting config can cost preferences but never a key.
const InvalidFileNextAction = "fix the JSON, or delete the file to start over; your stored API key is unaffected"

// Reported instead of an empty config: a file we cannot read must never look
// like "nothing configured yet".
func configInvalid(cause error, path, problem string) *lwerr.Error {
	return lwerr.Wrap(cause, lwerr.ConfigInvalid, fmt.Sprintf("the config file %s %s", path, problem), InvalidFileNextAction)
}

// ReadStoredConfig returns nil for "nothing configured yet". A file that exists
// but cannot be used is an error, because reading it as empty would silently
// drop the user's preferences.
func ReadStoredConfig(path string) (*StoredConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ENOTDIR) {
			return nil, nil
		}
		return nil, configInvalid(err, path, "cannot be read: "+err.Error())
	}
	// An empty file holds nothing worth preserving, so it counts as absent.
	if len(strings.TrimSpace(string(data))) == 0 {
		return nil, nil
	}
	var document json.RawMessage
	if err := json.Unmarshal(data, &document); err != nil {
		// SPEC §7 renders this failure literally, so the parse error stays on
		// the wrapped cause instead of riding along in the message.
		return nil, configInvalid(err, path, "is not valid JSON")
	}
	record, ok := AsRecord(document)
	if !ok {
		return nil, configInvalid(nil, path, "must hold a JSON object, found "+describeJSONValue(document))
	}
	stored := &StoredConfig{
		Repos:             sanitizeRepoPreferences(record.Get("repos")),
		Pins:              sanitizePinPreferences(record.Get("pins")),
		BranchNaming:      sanitizeBranchNaming(record.Get("branchNaming")),
		IssueProvider:     sanitizeProviderValue(record.Get("issueProvider")),
		WorktreeRoot:      sanitizePathValue(record.Get("worktreeRoot")),
		CredentialCommand: sanitizeCommandValue(record.Get("credentialCommand")),
		PruneMerged:       sanitizeBoolValue(record.Get("pruneMerged")),
	}
	return stored, nil
}

// BranchRuleFor returns the first repository rule matching keys, in caller
// priority order, plus the explicitly configured username variable.
func BranchRuleFor(stored *StoredConfig, keys ...string) (template, username string, ok bool) {
	_, template, username, ok = BranchRuleEntry(stored, keys...)
	return template, username, ok
}

// BranchRuleEntry also returns the key that matched, which lets management
// commands show or remove a path-keyed legacy rule when a remote key is absent.
func BranchRuleEntry(stored *StoredConfig, keys ...string) (repository, template, username string, ok bool) {
	if stored == nil || stored.BranchNaming == nil {
		return "", "", "", false
	}
	username = stored.BranchNaming.Variables.Username
	for _, key := range keys {
		if rule, found := stored.BranchNaming.ByRepository[key]; found && rule.Template != "" {
			return key, rule.Template, username, true
		}
	}
	return "", "", username, false
}

// SetBranchRule writes one repository rule without disturbing routing, pins,
// cleanup, or credential preferences. It reports whether anything changed.
func SetBranchRule(update BranchRuleUpdate, path string) (bool, error) {
	repository := strings.TrimSpace(update.Repository)
	template := strings.TrimSpace(update.Template)
	username := strings.TrimSpace(update.Username)
	current, err := readOrEmpty(path)
	if err != nil {
		return false, err
	}
	naming := BranchNaming{ByRepository: map[string]BranchRule{}}
	if current.BranchNaming != nil {
		naming = *current.BranchNaming
		if naming.ByRepository == nil {
			naming.ByRepository = map[string]BranchRule{}
		}
	}
	changed := naming.ByRepository[repository].Template != template
	naming.ByRepository[repository] = BranchRule{Template: template}
	if username != "" && naming.Variables.Username != username {
		naming.Variables.Username = username
		changed = true
	}
	if !changed {
		return false, nil
	}
	current.BranchNaming = &naming
	if err := Write(current, path); err != nil {
		return false, err
	}
	return true, nil
}

// UnsetBranchRule removes only the named repository rule. The username is kept
// because another repository rule may use it later.
func UnsetBranchRule(repository, path string) (bool, error) {
	repository = strings.TrimSpace(repository)
	current, err := readOrEmpty(path)
	if err != nil {
		return false, err
	}
	if current.BranchNaming == nil {
		return false, nil
	}
	if _, found := current.BranchNaming.ByRepository[repository]; !found {
		return false, nil
	}
	delete(current.BranchNaming.ByRepository, repository)
	if len(current.BranchNaming.ByRepository) == 0 {
		current.BranchNaming.ByRepository = nil
		if current.BranchNaming.Variables.Username == "" {
			current.BranchNaming = nil
		}
	}
	if err := Write(current, path); err != nil {
		return false, err
	}
	return true, nil
}

// RepoRoots are the directories the repo picker scans, tilde-expanded and made
// absolute. Empty means nothing is configured, and the caller decides what to
// offer instead.
func RepoRoots(stored *StoredConfig, env map[string]string) []string {
	if stored == nil || stored.Repos == nil {
		return nil
	}
	roots := make([]string, 0, len(stored.Repos.Roots))
	for _, root := range stored.Repos.Roots {
		roots = append(roots, ResolveConfiguredPath(root, env))
	}
	return roots
}

// AddRepoRoot saves a root entered in the picker without disturbing recents or
// any other configuration. Existing roots are left in their original order.
func AddRepoRoot(root, path string) error {
	root = strings.TrimSpace(root)
	if root == "" {
		return nil
	}
	current, err := readOrEmpty(path)
	if err != nil {
		return err
	}
	repos := RepoPreferences{}
	if current.Repos != nil {
		repos = *current.Repos
	}
	for _, existing := range repos.Roots {
		if existing == root {
			return nil
		}
	}
	repos.Roots = append(repos.Roots, root)
	current.Repos = &repos
	return Write(current, path)
}

// RecentRepos is what the user picked before, newest first.
func RecentRepos(stored *StoredConfig) []RecentRepo {
	if stored == nil || stored.Repos == nil {
		return nil
	}
	return stored.Repos.Recent
}

// PinnedProjects and PinnedTeams return stable IDs in user pin order.
func PinnedProjects(stored *StoredConfig) []string {
	if stored == nil || stored.Pins == nil {
		return nil
	}
	return append([]string(nil), stored.Pins.Projects...)
}

func PinnedTeams(stored *StoredConfig) []string {
	if stored == nil || stored.Pins == nil {
		return nil
	}
	return append([]string(nil), stored.Pins.Teams...)
}

func ToggleProjectPin(projectID, path string) (PinToggle, error) {
	return togglePin(projectID, path, true)
}

func ToggleTeamPin(teamID, path string) (PinToggle, error) {
	return togglePin(teamID, path, false)
}

func togglePin(id, path string, project bool) (PinToggle, error) {
	id = strings.TrimSpace(id)
	current, err := readOrEmpty(path)
	if err != nil {
		return PinToggle{}, err
	}
	pins := PinPreferences{}
	if current.Pins != nil {
		pins = *current.Pins
	}
	var existing []string
	if project {
		existing = pins.Projects
	} else {
		existing = pins.Teams
	}
	if id == "" {
		return PinToggle{IDs: append([]string(nil), existing...)}, nil
	}
	pinned := true
	next := make([]string, 0, len(existing)+1)
	for _, stored := range existing {
		if stored == id {
			pinned = false
			continue
		}
		next = append(next, stored)
	}
	if pinned {
		next = append(next, id)
	}
	if len(next) > PinPreferencesLimit {
		next = next[len(next)-PinPreferencesLimit:]
	}
	if project {
		pins.Projects = next
	} else {
		pins.Teams = next
	}
	if len(pins.Projects) == 0 && len(pins.Teams) == 0 {
		current.Pins = nil
	} else {
		current.Pins = &pins
	}
	if err := Write(current, path); err != nil {
		return PinToggle{}, err
	}
	return PinToggle{Pinned: pinned, IDs: append([]string(nil), next...)}, nil
}

// ProjectRepoPath returns the last repository selected for a project.
func ProjectRepoPath(stored *StoredConfig, projectID string) string {
	projectID = strings.TrimSpace(projectID)
	if projectID == "" || stored == nil || stored.Repos == nil {
		return ""
	}
	for _, association := range stored.Repos.Projects {
		if association.ProjectID == projectID {
			return association.Path
		}
	}
	return ""
}

// TeamRepoPath returns the repository last selected for projectless issues in a
// team. It is never preferred over a project-specific association.
func TeamRepoPath(stored *StoredConfig, teamID string) string {
	teamID = strings.TrimSpace(teamID)
	if teamID == "" || stored == nil || stored.Repos == nil {
		return ""
	}
	for _, association := range stored.Repos.Teams {
		if association.TeamID == teamID {
			return association.Path
		}
	}
	return ""
}

// RecordRepoUse moves a repository to the front of the recents and records an
// issue-scope association in the same config rewrite. A project wins; the team
// fallback is recorded only for a projectless issue. Blank paths are a no-op.
func RecordRepoUse(use RepoUse, path string, now int64) ([]RecentRepo, error) {
	repoPath := strings.TrimSpace(use.Path)
	projectID := strings.TrimSpace(use.ProjectID)
	teamID := strings.TrimSpace(use.TeamID)
	current, err := readOrEmpty(path)
	if err != nil {
		return nil, err
	}
	repos := RepoPreferences{}
	if current.Repos != nil {
		repos = *current.Repos
	}
	if repoPath == "" {
		return repos.Recent, nil
	}
	recent := []RecentRepo{{Path: repoPath, UsedAt: now}}
	for _, stored := range repos.Recent {
		if stored.Path == repoPath {
			continue
		}
		recent = append(recent, stored)
	}
	if len(recent) > RecentPreferencesLimit {
		recent = recent[:RecentPreferencesLimit]
	}
	repos.Recent = recent
	if projectID != "" {
		projects := []ProjectRepo{{ProjectID: projectID, Path: repoPath, UsedAt: now}}
		for _, stored := range repos.Projects {
			if stored.ProjectID != projectID {
				projects = append(projects, stored)
			}
		}
		if len(projects) > RecentPreferencesLimit {
			projects = projects[:RecentPreferencesLimit]
		}
		repos.Projects = projects
	} else if teamID != "" {
		teams := []TeamRepo{{TeamID: teamID, Path: repoPath, UsedAt: now}}
		for _, stored := range repos.Teams {
			if stored.TeamID != teamID {
				teams = append(teams, stored)
			}
		}
		if len(teams) > RecentPreferencesLimit {
			teams = teams[:RecentPreferencesLimit]
		}
		repos.Teams = teams
	}
	current.Repos = &repos
	if err := Write(current, path); err != nil {
		return nil, err
	}
	return recent, nil
}

// ResolveWorktreeRoot expands a leading "~" and makes the configured worktree
// root absolute, falling back to DefaultWorktreeRoot.
func ResolveWorktreeRoot(stored *StoredConfig, env map[string]string) string {
	configured := DefaultWorktreeRoot
	if stored != nil && stored.WorktreeRoot != "" {
		configured = stored.WorktreeRoot
	}
	return ResolveConfiguredPath(configured, env)
}

// SetPruneMerged stores the automatic-cleanup preference and reports whether
// anything changed, so a caller can say "already on" rather than claim a write
// it did not make. Like every mutator here it re-reads first, so a concurrent
// run's pin is not clobbered (SPEC §7).
func SetPruneMerged(enabled bool, path string) (bool, error) {
	current, err := readOrEmpty(path)
	if err != nil {
		return false, err
	}
	if current.PruneMerged == enabled {
		return false, nil
	}
	current.PruneMerged = enabled
	if err := Write(current, path); err != nil {
		return false, err
	}
	return true, nil
}

// ReadPruneMerged reports whether finished worktrees should be removed
// automatically. A missing key, a missing file, or anything that is not true
// all mean no: the safe answer is the one that deletes nothing.
func ReadPruneMerged(path string) (bool, error) {
	stored, err := ReadStoredConfig(path)
	if err != nil {
		return false, err
	}
	if stored == nil {
		return false, nil
	}
	return stored.PruneMerged, nil
}

// Write serialises with two-space indentation and a trailing newline, so the
// file stays hand-editable. No sanitisation happens here: the caller owns the
// shape of what it passes.
func Write(config *StoredConfig, path string) error {
	payload, err := MarshalIndented(config)
	if err != nil {
		return err
	}
	return AtomicWriteJSON(path, payload)
}

func readOrEmpty(path string) (*StoredConfig, error) {
	current, err := ReadStoredConfig(path)
	if err != nil {
		return nil, err
	}
	if current == nil {
		return &StoredConfig{}, nil
	}
	return current, nil
}

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
	prefs := &RepoPreferences{}
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
			prefs.Recent = append(prefs.Recent, RecentRepo{Path: path, UsedAt: int64(usedAt)})
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
	if len(prefs.Roots) == 0 && len(prefs.Recent) == 0 && len(prefs.Projects) == 0 && len(prefs.Teams) == 0 {
		return nil
	}
	return prefs
}

func sanitizeBranchNaming(raw json.RawMessage) *BranchNaming {
	record, ok := AsRecord(raw)
	if !ok {
		return nil
	}
	result := &BranchNaming{ByRepository: map[string]BranchRule{}}
	if variables, ok := AsRecord(record.Get("variables")); ok {
		result.Variables.Username = sanitizeCommandValue(variables.Get("username"))
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
				result.ByRepository[name] = BranchRule{Template: template}
			}
		}
	}
	if result.Variables.Username == "" && len(result.ByRepository) == 0 {
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
	}
	if len(pins.Projects) == 0 && len(pins.Teams) == 0 {
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
		candidate := ProjectRepo{ProjectID: projectID, Path: path, UsedAt: int64(usedAt)}
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
		candidate := TeamRepo{TeamID: teamID, Path: path, UsedAt: int64(usedAt)}
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
