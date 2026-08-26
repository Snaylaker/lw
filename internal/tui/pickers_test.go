package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/snaylaker/lw/internal/domain"
)

var testIssues = []domain.Issue{
	{ID: "issue-1", Identifier: "DEMO-4009", Title: "Improve workspace startup prompt", StateType: "unstarted", StateName: "Todo", TeamID: "team-demo", TeamKey: "DEMO", TeamName: "Developer Experience", ProjectID: "project-cli", ProjectName: "CLI Reliability"},
	{ID: "issue-2", Identifier: "DEMO-4007", Title: "Repository scan timeout", StateType: "triage", StateName: "Triage", TeamID: "team-demo", TeamKey: "DEMO", TeamName: "Developer Experience"},
}

func TestIssuePickerStartsAsOneWorkspaceSearchInput(t *testing.T) {
	picker := NewIssuePicker(IssuePickerOptions{})
	if picker.Breadcrumb() != "Find a Linear issue" {
		t.Fatalf("title = %q", picker.Breadcrumb())
	}
	if picker.ListStatus() != "type at least 2 characters" {
		t.Fatalf("status = %q", picker.ListStatus())
	}
	frame := plain(picker.View())
	mustContain(t, frame, "Issues  Projects  Teams")
	mustContain(t, frame, "[Tab] next view")
	mustContain(t, frame, "issue identifier or title")
	mustContain(t, frame, "[Ctrl+R] search again")
}

func TestProjectIssuePickerFiltersLocallyWithoutSchedulingSearch(t *testing.T) {
	picker := NewIssuePicker(IssuePickerOptions{Local: true, Title: "Issues in CLI Reliability"})
	picker.SetIssues(testIssues)
	for _, r := range "timeout" {
		if cmd := picker.Update(runeKey(r)); cmd != nil {
			t.Fatal("local filtering scheduled a remote command")
		}
	}
	mustRowLabels(t, picker.Rows(), "DEMO-4007 Repository scan timeout")
	frame := plain(picker.View())
	mustContain(t, frame, "Issues in CLI Reliability")
	mustContain(t, frame, "[Ctrl+R] reload")
}

func TestProjectPickerFiltersActiveProjectsLocally(t *testing.T) {
	picker := NewProjectPicker(ProjectPickerOptions{})
	picker.SetProjects([]domain.Project{
		{ID: "cli", Name: "CLI Reliability", StatusName: "In Progress"},
		{ID: "auth", Name: "Authentication", StatusName: "Planned"},
	})
	for _, r := range "cli" {
		picker.Update(runeKey(r))
	}
	mustRowLabels(t, picker.Rows(), "CLI Reliability")
	frame := plain(picker.View())
	mustContain(t, frame, "● Projects")
	mustContain(t, frame, "[Tab] next view")
}

func TestProjectPinsRankFirstAndToggleWithCtrlP(t *testing.T) {
	var toggled string
	picker := NewProjectPicker(ProjectPickerOptions{
		PinnedIDs: []string{"auth"},
		OnTogglePin: func(project domain.Project) ([]string, error) {
			toggled = project.ID
			return nil, nil
		},
	})
	picker.SetProjects([]domain.Project{
		{ID: "cli", Name: "CLI Reliability"},
		{ID: "auth", Name: "Authentication"},
	})
	mustRowLabels(t, picker.Rows(), "★ Authentication", "CLI Reliability")
	mustContain(t, picker.StatusLine(), "1 pinned")
	picker.Update(typedKey(tea.KeyCtrlP))
	if toggled != "auth" {
		t.Fatalf("toggled = %q", toggled)
	}
	mustRowLabels(t, picker.Rows(), "CLI Reliability", "Authentication")
	mustNotContain(t, picker.StatusLine(), "pinned")
}

func TestTeamPickerFiltersByNameOrKey(t *testing.T) {
	picker := NewTeamPicker(TeamPickerOptions{})
	picker.SetTeams([]domain.Team{
		{ID: "demo", Key: "DEMO", Name: "Developer Experience"},
		{ID: "eng", Key: "ENG", Name: "Engineering"},
	})
	for _, r := range "DEMO" {
		picker.Update(runeKey(r))
	}
	mustRowLabels(t, picker.Rows(), "Developer Experience")
	frame := plain(picker.View())
	mustContain(t, frame, "Find a Linear team")
	mustContain(t, frame, "[Enter] open issues")
}

func TestTeamPinsRankFirstAndToggleWithCtrlP(t *testing.T) {
	var toggled string
	picker := NewTeamPicker(TeamPickerOptions{
		PinnedIDs: []string{"eng"},
		OnTogglePin: func(team domain.Team) ([]string, error) {
			toggled = team.ID
			return nil, nil
		},
	})
	picker.SetTeams([]domain.Team{
		{ID: "demo", Key: "DEMO", Name: "Developer Experience"},
		{ID: "eng", Key: "ENG", Name: "Engineering"},
	})
	mustRowLabels(t, picker.Rows(), "★ Engineering", "Developer Experience")
	picker.Update(typedKey(tea.KeyCtrlP))
	if toggled != "eng" {
		t.Fatalf("toggled = %q", toggled)
	}
	mustRowLabels(t, picker.Rows(), "Developer Experience", "Engineering")
}

func TestIssuePickerWaitsForTwoCharactersThenShowsSearching(t *testing.T) {
	picker := NewIssuePicker(IssuePickerOptions{})
	picker.Update(runeKey('S'))
	if picker.ListStatus() != "type at least 2 characters" {
		t.Fatalf("one-character status = %q", picker.ListStatus())
	}
	if cmd := picker.Update(runeKey('I')); cmd == nil {
		t.Fatal("the ready query did not schedule a debounced search")
	}
	if picker.ListStatus() != "searching Linear…" {
		t.Fatalf("ready status = %q", picker.ListStatus())
	}
}

func TestIssuePickerPreservesLinearRankingAndShowsProjectOrTeam(t *testing.T) {
	picker := NewIssuePicker(IssuePickerOptions{})
	picker.SetQuery("DEMO")
	picker.SetIssues(testIssues)

	mustRowLabels(t, picker.Rows(),
		"DEMO-4009 Improve workspace startup prompt",
		"DEMO-4007 Repository scan timeout",
	)
	if got := picker.Rows()[0].Hint; got != "Todo · CLI Reliability" {
		t.Errorf("project hint = %q", got)
	}
	if got := picker.Rows()[1].Hint; got != "Triage · Developer Experience" {
		t.Errorf("team hint = %q", got)
	}
	if picker.StatusLine() != "2 results" {
		t.Errorf("status = %q", picker.StatusLine())
	}
}

func TestIssuePickerExplainsAnEmptySearch(t *testing.T) {
	picker := NewIssuePicker(IssuePickerOptions{})
	picker.SetQuery("DEMO")
	picker.SetIssues(nil)
	if picker.ListStatus() != "no matching active issues" || picker.StatusLine() != "0 results" {
		t.Fatalf("list/status = %q / %q", picker.ListStatus(), picker.StatusLine())
	}
}

func TestIssuePickerDoesNotLocallyDiscardSemanticResults(t *testing.T) {
	picker := NewIssuePicker(IssuePickerOptions{})
	picker.SetQuery("authentication")
	picker.SetIssues([]domain.Issue{{
		ID: "issue-1", Identifier: "DEMO-1", Title: "Login failure", StateName: "Todo",
	}})
	mustRowLabels(t, picker.Rows(), "DEMO-1 Login failure")
}

func TestIssuePickerKeepsHighlightAndQueryAcrossServerResults(t *testing.T) {
	var chosen []domain.Issue
	picker := NewIssuePicker(IssuePickerOptions{OnSelect: func(issue domain.Issue) {
		chosen = append(chosen, issue)
	}})
	picker.SetQuery("DEMO")
	picker.SetIssues(testIssues)
	picker.Update(typedKey(tea.KeyDown))
	mustHighlight(t, picker, "issue-2")

	later := append([]domain.Issue{{ID: "issue-0", Identifier: "DEMO-4010", Title: "New", StateName: "Todo"}}, testIssues...)
	picker.SetIssues(later)
	if picker.Query() != "DEMO" {
		t.Errorf("query = %q", picker.Query())
	}
	mustHighlight(t, picker, "issue-2")

	picker.Update(typedKey(tea.KeyEnter))
	if len(chosen) != 1 || chosen[0].ID != "issue-2" {
		t.Fatalf("selected = %+v", chosen)
	}
}

func TestIssuePickerTruncatesLongTitlesGraphemeSafely(t *testing.T) {
	picker := NewIssuePicker(IssuePickerOptions{})
	picker.SetQuery("long")
	picker.SetIssues([]domain.Issue{{
		ID: "i", Identifier: "DEMO-1", Title: strings.Repeat("👩‍👩‍👦", 100), StateName: "Backlog",
	}})
	mustRowLabels(t, picker.Rows(), "DEMO-1 "+strings.Repeat("👩‍👩‍👦", 79)+"…")
}
