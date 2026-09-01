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
	"strings"
	"syscall"

	"github.com/snaylaker/lw/internal/lwerr"
	issueprovider "github.com/snaylaker/lw/provider"
)

// RecentRepo is a repository the user picked before. Newest first.
type RecentRepo struct {
	Path    string `json:"path"`
	UsedAt  int64  `json:"usedAt"`
	unknown Record
}

// ProjectRepo remembers the repository selected for a Linear project. It is a
// durable local preference rather than data derived from a search result.
type ProjectRepo struct {
	ProjectID string `json:"projectId"`
	Path      string `json:"path"`
	UsedAt    int64  `json:"usedAt"`
	unknown   Record
}

// TeamRepo is the fallback for a projectless issue. Project associations stay
// more specific; a team association is consulted only when no project exists.
type TeamRepo struct {
	TeamID  string `json:"teamId"`
	Path    string `json:"path"`
	UsedAt  int64  `json:"usedAt"`
	unknown Record
}

// RepoUse is one provider scope/repository choice recorded atomically.
type RepoUse struct {
	Provider issueprovider.ID
	Scopes   []issueprovider.Scope
	Path     string
}

// RepoPreferences is where the repo picker looks. Roots are scanned one level
// deep; Recent orders the picker, Projects route project issues, and Teams route
// projectless issues.
type RepoPreferences struct {
	Roots    []string      `json:"roots,omitempty"`
	Recent   []RecentRepo  `json:"recent,omitempty"`
	Projects []ProjectRepo `json:"projects,omitempty"`
	Teams    []TeamRepo    `json:"teams,omitempty"`
	unknown  Record
}

// PinPreferences keeps stable Linear IDs, not list responses. Pins therefore
// survive refreshes without becoming a cache of workspace data.
type PinPreferences struct {
	Projects []string `json:"projects,omitempty"`
	Teams    []string `json:"teams,omitempty"`
	unknown  Record
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
	unknown  Record
}

// BranchVariables holds explicit values shared by branch templates. Username
// is configured rather than guessed from git's human-readable user.name.
type BranchVariables struct {
	Username string `json:"username,omitempty"`
	unknown  Record
}

// BranchNaming maps a stable remote repository key (for example
// gitlab.example.com/group/repo) or an absolute checkout path to its rule.
type BranchNaming struct {
	Variables    BranchVariables       `json:"variables,omitempty"`
	ByRepository map[string]BranchRule `json:"byRepository,omitempty"`
	unknown      Record
}

// BranchRuleUpdate is one atomic repository-rule change. A blank Username
// preserves the existing global template variable.
type BranchRuleUpdate struct {
	Repository string
	Template   string
	Username   string
}

// StoredConfig is the whole file. Every section is optional, and unknown keys
// survive writes so a newer config can safely be edited by an older lw binary.
// No secret is ever stored here: CredentialCommand names a way to *fetch* the
// key, and the key itself never comes back to this file.
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
	unknown     Record
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
		unknown: unknownExcept(record,
			"repos", "pins", "branchNaming", "issueProvider", "worktreeRoot", "credentialCommand", "pruneMerged",
			"clientId", "redirectPort"),
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
	return updateConfig(path, func(current *StoredConfig) (bool, bool, error) {
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
		if changed {
			current.BranchNaming = &naming
		}
		return changed, changed, nil
	})
}

// UnsetBranchRule removes only the named repository rule. The username is kept
// because another repository rule may use it later.
func UnsetBranchRule(repository, path string) (bool, error) {
	repository = strings.TrimSpace(repository)
	return updateConfig(path, func(current *StoredConfig) (bool, bool, error) {
		if current.BranchNaming == nil {
			return false, false, nil
		}
		if _, found := current.BranchNaming.ByRepository[repository]; !found {
			return false, false, nil
		}
		delete(current.BranchNaming.ByRepository, repository)
		if len(current.BranchNaming.ByRepository) == 0 {
			current.BranchNaming.ByRepository = nil
			if current.BranchNaming.Variables.Username == "" {
				current.BranchNaming = nil
			}
		}
		return true, true, nil
	})
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
	_, err := updateConfig(path, func(current *StoredConfig) (struct{}, bool, error) {
		repos := RepoPreferences{}
		if current.Repos != nil {
			repos = *current.Repos
		}
		for _, existing := range repos.Roots {
			if existing == root {
				return struct{}{}, false, nil
			}
		}
		repos.Roots = append(repos.Roots, root)
		current.Repos = &repos
		return struct{}{}, true, nil
	})
	return err
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
	return updateConfig(path, func(current *StoredConfig) (PinToggle, bool, error) {
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
			return PinToggle{IDs: append([]string(nil), existing...)}, false, nil
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
		return PinToggle{Pinned: pinned, IDs: append([]string(nil), next...)}, true, nil
	})
}

// RepoPath returns the repository associated with the issue's most specific
// provider scope. The project/team storage below is retained as an on-disk
// compatibility detail for existing config files.
func RepoPath(stored *StoredConfig, provider issueprovider.ID, scopes []issueprovider.Scope) string {
	projectID, teamID := routingIDs(provider, scopes)
	if projectID != "" {
		return ProjectRepoPath(stored, projectID)
	}
	return TeamRepoPath(stored, teamID)
}

func routingIDs(provider issueprovider.ID, scopes []issueprovider.Scope) (projectID, teamID string) {
	for _, scope := range scopes {
		kind := strings.TrimSpace(scope.Kind)
		id := strings.TrimSpace(scope.ID)
		if kind == "" || id == "" {
			continue
		}
		switch kind {
		case "linear_project":
			if projectID == "" {
				projectID = id
			}
		case "linear_team":
			if teamID == "" {
				teamID = id
			}
		default:
			if projectID == "" {
				projectID = string(provider) + ":" + kind + ":" + id
			}
		}
	}
	return projectID, teamID
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
	projectID, teamID := routingIDs(use.Provider, use.Scopes)
	return updateConfig(path, func(current *StoredConfig) ([]RecentRepo, bool, error) {
		repos := RepoPreferences{}
		if current.Repos != nil {
			repos = *current.Repos
		}
		if repoPath == "" {
			return repos.Recent, false, nil
		}
		recent := []RecentRepo{{Path: repoPath, UsedAt: now}}
		for _, stored := range repos.Recent {
			if stored.Path != repoPath {
				recent = append(recent, stored)
			}
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
		return recent, true, nil
	})
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
// it did not make.
func SetPruneMerged(enabled bool, path string) (bool, error) {
	return updateConfig(path, func(current *StoredConfig) (bool, bool, error) {
		if current.PruneMerged == enabled {
			return false, false, nil
		}
		current.PruneMerged = enabled
		return true, true, nil
	})
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

type configMutation[T any] func(*StoredConfig) (result T, changed bool, err error)

// updateConfig serializes the complete read-modify-write transaction across
// processes. AtomicWriteJSON protects file integrity; this lock also prevents
// two valid concurrent updates from overwriting each other.
func updateConfig[T any](path string, mutate configMutation[T]) (T, error) {
	var zero T
	unlock, err := lockConfig(path)
	if err != nil {
		return zero, err
	}
	defer unlock()

	current, err := readOrEmpty(path)
	if err != nil {
		return zero, err
	}
	result, changed, err := mutate(current)
	if err != nil || !changed {
		return result, err
	}
	if err := Write(current, path); err != nil {
		return zero, err
	}
	return result, nil
}
