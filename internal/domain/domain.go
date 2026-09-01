// Package domain holds the value types shared across packages. It imports
// nothing from the rest of the tree, so every other package may depend on it.
package domain

// Project is one active Linear project in the optional project browser.
type Project struct {
	ID         string
	Name       string
	StatusName string
	UpdatedAt  string
}

// Team is one active Linear team in the optional team browser.
type Team struct {
	ID   string
	Key  string
	Name string
}

// Issue is one provider work item adapted for lw's existing flow. Identifier
// is the safe worktree directory key; Reference is the provider's human-facing
// name, such as ENG-3971 or owner/repository#123. The repository's actual Git
// branch is resolved separately.
type Issue struct {
	ID              string
	Provider        string
	ExternalID      string
	Identifier      string
	Reference       string
	Title           string
	URL             string
	StateType       string // triage | backlog | unstarted | started | completed | canceled
	StateName       string
	TeamID          string
	TeamKey         string
	TeamName        string
	ProjectID       string // empty when the issue has no project
	ProjectName     string
	SuggestedBranch string
	BranchKeys      []string
}

// DisplayReference keeps older in-process callers compatible while provider
// adapters supply a richer reference.
func (i Issue) DisplayReference() string {
	if i.Reference != "" {
		return i.Reference
	}
	return i.Identifier
}

// Branch is the git branch selected for an issue. ExistingLocal means it can be
// checked out directly. ExistingRemote names a remote-tracking ref from which
// lw must create the local branch. A new branch has neither and starts at Base.
type Branch struct {
	Name           string
	ExistingLocal  bool
	ExistingRemote string
	Base           string
}

// BranchResolution is the result of inspecting one repository. Selected is set
// for an explicit name, a configured template, or one unambiguous existing
// match. Multiple existing matches are Candidates. With no match, Suggested is
// the editable value shown by the interactive launcher.
type BranchResolution struct {
	Selected   *Branch
	Candidates []Branch
	Suggested  string
}

// Repo is a validated git checkout.
type Repo struct {
	Root string // absolute path of the toplevel
	Name string // basename of the toplevel
}

type StageID string

const (
	StagePreparing        StageID = "preparing"
	StageCreatingWorktree StageID = "creating-worktree"
)

type StageState string

const (
	StatePending StageState = "pending"
	StateActive  StageState = "active"
	StateDone    StageState = "done"
	StateFailed  StageState = "failed"
	StateSkipped StageState = "skipped"
)

type StageUpdate struct {
	Stage  StageID
	State  StageState
	Detail string
}

// FlowResult reports a finished worktree.
type FlowResult struct {
	CheckoutPath string
	// Created is false for a worktree that already existed and was reused.
	Created bool
}

// Credential carries the existing Linear personal API key boundary. Other
// providers own their read-only authentication inside their adapters.
type Credential struct {
	Key string
}
