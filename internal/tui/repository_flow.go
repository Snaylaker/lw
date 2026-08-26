package tui

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/snaylaker/lw/internal/domain"
)

// openRepos asks where the selected issue's branch belongs when no durable
// project/team association can answer.
func (m *Launcher) openRepos(issue domain.Issue) tea.Cmd {
	var repos []RankedRepo
	if m.deps.ListRepos != nil {
		repos = m.deps.ListRepos()
	}
	if len(repos) == 0 {
		return m.openRoot(func() tea.Cmd { return m.openRepos(issue) })
	}

	m.screen = ScreenRepos
	picker := NewRepoPicker(RepoPickerOptions{
		Context:  issue.Identifier,
		OnSelect: func(repo domain.Repo) { m.enqueue(m.chooseRepo(repo)) },
	})
	m.show(picker)
	m.repoPicker = picker
	picker.SetRepos(repos)
	return picker.FocusInput()
}

func (m *Launcher) chooseRepo(repo domain.Repo) tea.Cmd {
	if m.currentIssue == nil {
		return m.openIssues()
	}
	issue := *m.currentIssue
	safelyRecordRepo(m.deps.RecordRepoUse, issue, repo)
	chosen := repo
	m.currentRepo = &chosen
	return m.startFlow(issue)
}

func (m *Launcher) chosenRepo() domain.Repo {
	if m.currentRepo != nil {
		return *m.currentRepo
	}
	if m.deps.PreselectRepo != nil {
		return *m.deps.PreselectRepo
	}
	return m.deps.Repo
}

// Repository recents are advisory: a broken recorder must not lose a valid
// issue selection.
func safelyRecordRepo(record func(domain.Issue, domain.Repo), issue domain.Issue, repo domain.Repo) {
	if record == nil {
		return
	}
	defer func() { _ = recover() }()
	record(issue, repo)
}
