package tui

import tea "github.com/charmbracelet/bubbletea"

func (m *Launcher) onKey(msg tea.KeyMsg) (bool, tea.Cmd) {
	if m.settled {
		return true, nil
	}
	key := describeKey(msg)
	switch {
	case key.ctrl && key.name == "c":
		return true, m.cancel()
	case key.name == "tab" && !key.ctrl && !key.alt && (m.deps.BrowseCollections || m.deps.ProviderName == ""):
		return m.cycleCollections()
	case key.name == "escape":
		return true, m.goBack()
	case key.name == "r" && key.ctrl && !key.alt:
		return m.reload()
	case m.screen == ScreenError && m.retryAction != nil && key.name == "r" && !key.ctrl && !key.alt:
		retry := m.retryAction
		m.retryAction = nil
		return true, retry()
	default:
		return false, nil
	}
}

func (m *Launcher) cancel() tea.Cmd {
	if m.flowRunning && m.flowCancel != nil {
		if !m.flowAborted {
			m.flowAborted = true
			if m.progress != nil {
				m.progress.ShowCancelling()
			}
			m.flowCancel()
		}
		return nil
	}
	m.abortLoad()
	m.settle(LauncherOutcome{Cancelled: true})
	return nil
}

func (m *Launcher) cycleCollections() (bool, tea.Cmd) {
	switch m.screen {
	case ScreenIssues:
		m.abortLoad()
		m.rememberIssueQuery()
		m.returnSource = m.issueSource
		return true, m.openProjects()
	case ScreenProjects:
		m.abortLoad()
		if m.projectPicker != nil {
			m.projectQuery = m.projectPicker.Query()
		}
		return true, m.openTeams()
	case ScreenTeams:
		m.abortLoad()
		if m.teamPicker != nil {
			m.teamQuery = m.teamPicker.Query()
		}
		return true, m.reopenIssueView()
	default:
		return false, nil
	}
}

func (m *Launcher) rememberIssueQuery() {
	if m.issuePicker == nil {
		return
	}
	switch m.issueSource.kind {
	case issueSourceProject:
		m.projectIssueQuery = m.issuePicker.Query()
	case issueSourceTeam:
		m.teamIssueQuery = m.issuePicker.Query()
	default:
		m.searchQuery = m.issuePicker.Query()
	}
}

func (m *Launcher) goBack() tea.Cmd {
	m.abortLoad()
	switch m.screen {
	case ScreenCredential, ScreenCredentialSaved, ScreenProjects, ScreenTeams:
		m.settle(LauncherOutcome{Cancelled: true})
	case ScreenIssues:
		m.rememberIssueQuery()
		m.returnSource = m.issueSource
		switch m.issueSource.kind {
		case issueSourceProject:
			return m.openProjects()
		case issueSourceTeam:
			return m.openTeams()
		default:
			m.settle(LauncherOutcome{Cancelled: true})
		}
	case ScreenRoot:
		if m.currentIssue != nil {
			return m.openIssues()
		}
		m.settle(LauncherOutcome{Cancelled: true})
	case ScreenRepos, ScreenBranchLoading, ScreenBranches, ScreenBranchInput:
		m.currentIssue = nil
		m.currentRepo = nil
		m.returnSource = m.repoSource
		return m.reopenIssueView()
	case ScreenError:
		m.settle(LauncherOutcome{})
	}
	return nil
}

func (m *Launcher) reload() (bool, tea.Cmd) {
	switch {
	case m.screen == ScreenProjects && m.projectPicker != nil:
		m.projectPicker.SetLoading()
		m.projectsLoaded = false
		return true, m.loadProjects()
	case m.screen == ScreenTeams && m.teamPicker != nil:
		m.teamPicker.SetLoading()
		m.teamsLoaded = false
		return true, m.loadTeams()
	case m.screen == ScreenIssues && m.issuePicker != nil && m.issueSource.kind == issueSourceProject:
		m.issuePicker.SetLoading()
		m.projectIssuesLoadedID = ""
		return true, m.loadProjectIssues(m.issueSource.project)
	case m.screen == ScreenIssues && m.issuePicker != nil && m.issueSource.kind == issueSourceTeam:
		m.issuePicker.SetLoading()
		m.teamIssuesLoadedID = ""
		return true, m.loadTeamIssues(m.issueSource.team)
	case m.screen == ScreenIssues && m.issuePicker != nil:
		query := m.issuePicker.Query()
		if searchQueryReady(query) {
			m.issuePicker.SetSearching()
			return true, m.searchIssues(query)
		}
		return true, nil
	default:
		return false, nil
	}
}
