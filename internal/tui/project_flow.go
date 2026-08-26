package tui

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/snaylaker/lw/internal/domain"
	"github.com/snaylaker/lw/internal/lwerr"
)

func (m *Launcher) openProjects() tea.Cmd {
	m.screen = ScreenProjects
	picker := NewProjectPicker(ProjectPickerOptions{
		OnSelect:  func(project domain.Project) { m.enqueue(m.openProjectIssues(project)) },
		PinnedIDs: m.deps.ProjectPins,
		OnTogglePin: func(project domain.Project) ([]string, error) {
			if m.deps.ToggleProjectPin == nil {
				return m.deps.ProjectPins, nil
			}
			ids, err := m.deps.ToggleProjectPin(project)
			if err == nil {
				m.deps.ProjectPins = append([]string(nil), ids...)
			}
			return ids, err
		},
	})
	m.show(picker)
	m.projectPicker = picker
	picker.SetQuery(m.projectQuery)
	if m.projectsLoaded {
		picker.SetProjects(m.projectItems)
		return picker.FocusInput()
	}
	picker.SetLoading()
	return tea.Batch(picker.FocusInput(), m.loadProjects())
}

func (m *Launcher) loadProjects() tea.Cmd {
	ctx, token := m.beginLoad()
	list := m.deps.ListProjects
	return func() tea.Msg {
		if list == nil {
			return projectsLoadedMsg{token: token, err: lwerr.New(
				lwerr.Internal, "Linear project search is unavailable.", "report this: it is a bug in lw")}
		}
		items, err := list(ctx)
		return projectsLoadedMsg{token: token, items: items, err: err}
	}
}

func (m *Launcher) onProjectsLoaded(msg projectsLoadedMsg) tea.Cmd {
	if m.settled || msg.token != m.loadToken || m.screen != ScreenProjects || m.projectPicker == nil {
		return nil
	}
	m.cancelLoad = nil
	if msg.err != nil {
		m.projectQuery = m.projectPicker.Query()
		m.handleFailure(msg.err, m.openProjects)
		return nil
	}
	m.projectItems = append([]domain.Project(nil), msg.items...)
	m.projectsLoaded = true
	m.projectPicker.SetProjects(msg.items)
	return nil
}

func (m *Launcher) openProjectIssues(project domain.Project) tea.Cmd {
	if m.projectPicker != nil {
		m.projectQuery = m.projectPicker.Query()
	}
	if m.currentProject == nil || m.currentProject.ID != project.ID {
		m.projectIssueQuery = ""
	}
	selected := project
	m.currentProject = &selected
	m.issueViewProject = true
	m.issueViewTeam = false
	m.returnToProjectIssues = true
	m.returnToTeamIssues = false
	m.screen = ScreenIssues
	picker := NewIssuePicker(IssuePickerOptions{
		Local:    true,
		Title:    "Issues in " + project.Name,
		OnSelect: func(issue domain.Issue) { m.enqueue(m.chooseIssue(issue)) },
	})
	m.show(picker)
	m.issuePicker = picker
	picker.SetQuery(m.projectIssueQuery)
	if m.projectIssuesLoadedID == project.ID {
		picker.SetIssues(m.projectIssueItems)
		return picker.FocusInput()
	}
	picker.SetLoading()
	return tea.Batch(picker.FocusInput(), m.loadProjectIssues(project))
}

func (m *Launcher) loadProjectIssues(project domain.Project) tea.Cmd {
	ctx, token := m.beginLoad()
	list := m.deps.ListProjectIssues
	return func() tea.Msg {
		if list == nil {
			return projectIssuesLoadedMsg{token: token, projectID: project.ID, err: lwerr.New(
				lwerr.Internal, "Linear project issues are unavailable.", "report this: it is a bug in lw")}
		}
		items, err := list(ctx, project)
		return projectIssuesLoadedMsg{token: token, projectID: project.ID, items: items, err: err}
	}
}

func (m *Launcher) onProjectIssuesLoaded(msg projectIssuesLoadedMsg) tea.Cmd {
	if m.settled || msg.token != m.loadToken || m.screen != ScreenIssues || !m.issueViewProject ||
		m.issuePicker == nil || m.currentProject == nil || msg.projectID != m.currentProject.ID {
		return nil
	}
	m.cancelLoad = nil
	if msg.err != nil {
		m.projectIssueQuery = m.issuePicker.Query()
		project := *m.currentProject
		m.handleFailure(msg.err, func() tea.Cmd { return m.openProjectIssues(project) })
		return nil
	}
	m.projectIssuesLoadedID = msg.projectID
	m.projectIssueItems = append([]domain.Issue(nil), msg.items...)
	m.issuePicker.SetIssues(msg.items)
	return nil
}

func (m *Launcher) reopenIssueView() tea.Cmd {
	if m.returnToProjectIssues && m.currentProject != nil {
		return m.openProjectIssues(*m.currentProject)
	}
	if m.returnToTeamIssues && m.currentTeam != nil {
		return m.openTeamIssues(*m.currentTeam)
	}
	return m.openIssues()
}
