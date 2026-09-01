package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/snaylaker/lw/internal/domain"
	"github.com/snaylaker/lw/internal/lwerr"
)

func (m *Launcher) openIssues() tea.Cmd {
	m.issueViewProject = false
	m.issueViewTeam = false
	m.returnToProjectIssues = false
	m.returnToTeamIssues = false
	m.screen = ScreenIssues
	picker := NewIssuePicker(IssuePickerOptions{
		OnSelect:          func(issue domain.Issue) { m.enqueue(m.chooseIssue(issue)) },
		ProviderName:      m.deps.ProviderName,
		BrowseCollections: m.deps.BrowseCollections,
	})
	m.show(picker)
	m.issuePicker = picker
	picker.SetQuery(m.searchQuery)
	query := strings.TrimSpace(m.searchQuery)
	if m.searchResultsLoaded && m.searchResultsQuery == query {
		picker.SetIssues(m.searchItems)
		return picker.FocusInput()
	}
	if searchQueryReady(query) {
		picker.SetSearching()
		return tea.Batch(picker.FocusInput(), m.searchIssues(query))
	}
	return picker.FocusInput()
}

func (m *Launcher) onIssueSearchDue(msg issueSearchDueMsg) tea.Cmd {
	if m.settled || m.screen != ScreenIssues || m.issueViewProject || m.issueViewTeam || m.issuePicker == nil {
		return nil
	}
	if msg.generation != m.issuePicker.generation || msg.query != m.issuePicker.Query() {
		return nil
	}
	return m.searchIssues(msg.query)
}

func (m *Launcher) searchIssues(query string) tea.Cmd {
	query = strings.TrimSpace(query)
	if !searchQueryReady(query) {
		return nil
	}
	ctx, token := m.beginLoad()
	search := m.deps.SearchIssues
	return func() tea.Msg {
		if search == nil {
			return issuesLoadedMsg{token: token, query: query, err: lwerr.New(
				lwerr.Internal, "Issue search is unavailable.", "report this: it is a bug in lw")}
		}
		items, err := search(ctx, query)
		return issuesLoadedMsg{token: token, query: query, items: items, err: err}
	}
}

func (m *Launcher) onIssuesLoaded(msg issuesLoadedMsg) tea.Cmd {
	if m.settled || msg.token != m.loadToken || m.screen != ScreenIssues || m.issueViewProject || m.issueViewTeam || m.issuePicker == nil || msg.query != strings.TrimSpace(m.issuePicker.Query()) {
		return nil
	}
	m.cancelLoad = nil
	if msg.err != nil {
		query := msg.query
		m.handleFailure(msg.err, func() tea.Cmd {
			m.searchQuery = query
			return m.openIssues()
		})
		return nil
	}
	m.searchQuery = msg.query
	m.searchItems = append([]domain.Issue(nil), msg.items...)
	m.searchResultsQuery = msg.query
	m.searchResultsLoaded = true
	m.issuePicker.SetIssues(msg.items)
	return nil
}

func (m *Launcher) chooseIssue(issue domain.Issue) tea.Cmd {
	m.repoIssueProject = m.issueViewProject
	m.repoIssueTeam = m.issueViewTeam
	if m.issuePicker != nil {
		if m.issueViewProject {
			m.projectIssueQuery = m.issuePicker.Query()
		} else if m.issueViewTeam {
			m.teamIssueQuery = m.issuePicker.Query()
		}
	}
	selected := issue
	m.currentIssue = &selected

	if m.deps.PreselectRepo != nil {
		repo := *m.deps.PreselectRepo
		m.currentRepo = &repo
		return m.prepareBranch(issue)
	}
	if m.deps.RepoForIssue != nil {
		if repo, ok := m.deps.RepoForIssue(issue); ok {
			chosen := repo
			m.currentRepo = &chosen
			safelyRecordRepo(m.deps.RecordRepoUse, issue, repo)
			return m.prepareBranch(issue)
		}
	}
	return m.openRepos(issue)
}
