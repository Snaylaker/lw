// Package provider defines the public issue-provider contract used by lw.
// Implementations resolve and search work items; they never create branches or
// worktrees. The Git behavior remains owned by lw after an item is returned.
package provider

import "context"

// ID is the stable configuration and metadata name of an issue provider.
type ID string

const (
	Linear ID = "linear"
	GitHub ID = "github"
	Jira   ID = "jira"
)

// Scope is a durable provider-side collection that can be associated with a
// local source repository. Examples are a Linear project, GitHub repository, or
// Jira project.
type Scope struct {
	Kind string
	ID   string
	Key  string
	Name string
}

// WorkItem is the provider-neutral issue data lw needs. WorktreeKey must be a
// safe, deterministic path segment. Reference is the human-facing provider
// reference, which may contain characters such as GitHub's slash and hash.
type WorkItem struct {
	Provider        ID
	ExternalID      string
	Reference       string
	WorktreeKey     string
	Title           string
	URL             string
	StateType       string
	StateName       string
	SuggestedBranch string
	BranchKeys      []string
	Scopes          []Scope
}

// Provider is the compile-time extension point. Go interfaces are satisfied
// implicitly: an implementation needs only these methods and does not import
// lw's Git or worktree packages.
type Provider interface {
	ID() ID
	DisplayName() string
	ValidateReference(reference string) error
	Resolve(ctx context.Context, reference string) (WorkItem, error)
	Search(ctx context.Context, query string) ([]WorkItem, error)
}
