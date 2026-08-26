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

// Issue is one Linear issue. The identifier ("ENG-3971") names the worktree
// directory and its branch.
type Issue struct {
	ID          string
	Identifier  string
	Title       string
	URL         string
	StateType   string // triage | backlog | unstarted | started | completed | canceled
	StateName   string
	TeamID      string
	TeamKey     string
	TeamName    string
	ProjectID   string // empty when the issue has no project
	ProjectName string
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

// Credential is the one supported authentication method: a personal Linear API
// key with Read permission. It is never written to a file or a process argument.
type Credential struct {
	Key string
}
