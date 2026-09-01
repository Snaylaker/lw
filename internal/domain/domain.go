// Package domain holds the value types shared across lw's internal packages.
package domain

import issueprovider "github.com/snaylaker/lw/provider"

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

// Issue is the provider-neutral work item used by the execution flow. The
// alias keeps one canonical model at the public provider boundary.
type Issue = issueprovider.WorkItem

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
