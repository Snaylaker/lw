package tui

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/snaylaker/lw/internal/domain"
	"github.com/snaylaker/lw/internal/lwerr"
)

func (m *Launcher) openTeams() tea.Cmd {
	m.screen = ScreenTeams
	picker := NewTeamPicker(TeamPickerOptions{
		OnSelect:  func(team domain.Team) { m.enqueue(m.openTeamIssues(team)) },
		PinnedIDs: m.deps.TeamPins,
		OnTogglePin: func(team domain.Team) ([]string, error) {
			if m.deps.ToggleTeamPin == nil {
				return m.deps.TeamPins, nil
			}
			ids, err := m.deps.ToggleTeamPin(team)
			if err == nil {
				m.deps.TeamPins = append([]string(nil), ids...)
			}
			return ids, err
		},
	})
	m.show(picker)
	m.teamPicker = picker
	picker.SetQuery(m.teamQuery)
	if m.teamsLoaded {
		picker.SetTeams(m.teamItems)
		return picker.FocusInput()
	}
	picker.SetLoading()
	return tea.Batch(picker.FocusInput(), m.loadTeams())
}

func (m *Launcher) loadTeams() tea.Cmd {
	ctx, token := m.beginLoad()
	list := m.deps.ListTeams
	return func() tea.Msg {
		if list == nil {
			return teamsLoadedMsg{token: token, err: lwerr.New(
				lwerr.Internal, "Linear team search is unavailable.", "report this: it is a bug in lw")}
		}
		items, err := list(ctx)
		return teamsLoadedMsg{token: token, items: items, err: err}
	}
}

func (m *Launcher) onTeamsLoaded(msg teamsLoadedMsg) tea.Cmd {
	if m.settled || msg.token != m.loadToken || m.screen != ScreenTeams || m.teamPicker == nil {
		return nil
	}
	m.cancelLoad = nil
	if msg.err != nil {
		m.teamQuery = m.teamPicker.Query()
		m.handleFailure(msg.err, m.openTeams)
		return nil
	}
	m.teamItems = append([]domain.Team(nil), msg.items...)
	m.teamsLoaded = true
	m.teamPicker.SetTeams(msg.items)
	return nil
}

func (m *Launcher) openTeamIssues(team domain.Team) tea.Cmd {
	if m.teamPicker != nil {
		m.teamQuery = m.teamPicker.Query()
	}
	if m.issueSource.kind != issueSourceTeam || m.issueSource.team.ID != team.ID {
		m.teamIssueQuery = ""
	}
	m.issueSource = teamSource(team)
	m.returnSource = m.issueSource
	m.screen = ScreenIssues
	picker := NewIssuePicker(IssuePickerOptions{
		Local:    true,
		Title:    "Issues in " + team.Name + " (" + team.Key + ")",
		OnSelect: func(issue domain.Issue) { m.enqueue(m.chooseIssue(issue)) },
	})
	m.show(picker)
	m.issuePicker = picker
	picker.SetQuery(m.teamIssueQuery)
	if m.teamIssuesLoadedID == team.ID {
		picker.SetIssues(m.teamIssueItems)
		return picker.FocusInput()
	}
	picker.SetLoading()
	return tea.Batch(picker.FocusInput(), m.loadTeamIssues(team))
}

func (m *Launcher) loadTeamIssues(team domain.Team) tea.Cmd {
	ctx, token := m.beginLoad()
	list := m.deps.ListTeamIssues
	return func() tea.Msg {
		if list == nil {
			return teamIssuesLoadedMsg{token: token, teamID: team.ID, err: lwerr.New(
				lwerr.Internal, "Linear team issues are unavailable.", "report this: it is a bug in lw")}
		}
		items, err := list(ctx, team)
		return teamIssuesLoadedMsg{token: token, teamID: team.ID, items: items, err: err}
	}
}

func (m *Launcher) onTeamIssuesLoaded(msg teamIssuesLoadedMsg) tea.Cmd {
	if m.settled || msg.token != m.loadToken || m.screen != ScreenIssues || m.issueSource.kind != issueSourceTeam ||
		m.issuePicker == nil || msg.teamID != m.issueSource.team.ID {
		return nil
	}
	m.cancelLoad = nil
	if msg.err != nil {
		m.teamIssueQuery = m.issuePicker.Query()
		team := m.issueSource.team
		m.handleFailure(msg.err, func() tea.Cmd { return m.openTeamIssues(team) })
		return nil
	}
	m.teamIssuesLoadedID = msg.teamID
	m.teamIssueItems = append([]domain.Issue(nil), msg.items...)
	m.issuePicker.SetIssues(msg.items)
	return nil
}
