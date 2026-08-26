package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/snaylaker/lw/internal/domain"
)

// RepoGroup orders the repo list: where you are standing, then what you used
// before, then everything discovered under the configured roots.
type RepoGroup string

const (
	RepoGroupCurrent    RepoGroup = "current"
	RepoGroupRecent     RepoGroup = "recent"
	RepoGroupDiscovered RepoGroup = "discovered"
)

// RankedRepo is one row of the repo picker, already grouped.
type RankedRepo struct {
	Repo  domain.Repo
	Group RepoGroup
}

type RepoPickerOptions struct {
	Context  string
	OnSelect func(domain.Repo)
}

// RepoPicker is the repository screen, shown after an issue is chosen. Unlike
// the other two lists it never touches the network: every row comes from disk,
// so it paints immediately and has no refresh.
type RepoPicker struct {
	list       *SearchableList
	breadcrumb string
	status     string

	reposByID map[string]domain.Repo
	rows      []SearchableItem
	painted   bool
}

func NewRepoPicker(options RepoPickerOptions) *RepoPicker {
	picker := &RepoPicker{
		breadcrumb: TruncateGraphemes(options.Context, 60) + " ▸ choose a repository",
		reposByID:  map[string]domain.Repo{},
	}
	picker.list = NewSearchableList(SearchableListOptions{
		Placeholder: "search repositories",
		EmptyText:   "no repositories found",
		LoadingText: "looking for repositories…",
		OnSelect: func(item SearchableItem) {
			if repo, ok := picker.reposByID[item.ID]; ok && options.OnSelect != nil {
				options.OnSelect(repo)
			}
		},
	})
	return picker
}

func (p *RepoPicker) Query() string { return p.list.Query() }

func (p *RepoPicker) SetLoading() { p.list.SetLoading(true) }

// SetRepos renders the grouped list. The hint is the path, because repositories
// with the same directory name still need to be distinguishable.
func (p *RepoPicker) SetRepos(repos []RankedRepo) {
	p.reposByID = make(map[string]domain.Repo, len(repos))
	rows := make([]SearchableItem, 0, len(repos))
	for _, ranked := range repos {
		p.reposByID[ranked.Repo.Root] = ranked.Repo
		label := ranked.Repo.Name
		if ranked.Group == RepoGroupCurrent {
			label = CurrentMarker + label
		}
		rows = append(rows, SearchableItem{
			ID:    ranked.Repo.Root,
			Label: TruncateGraphemes(label, 60),
			Hint:  ranked.Repo.Root,
		})
	}
	p.rows, p.painted = applyRows(p.list, p.rows, rows, p.painted)
	p.status = repoStatus(repos)
}

// CurrentMarker flags the repository the user is standing in.
const CurrentMarker = "● "

func repoStatus(repos []RankedRepo) string {
	if len(repos) == 0 {
		return ""
	}
	var current, recent, discovered int
	for _, ranked := range repos {
		switch ranked.Group {
		case RepoGroupCurrent:
			current++
		case RepoGroupRecent:
			recent++
		default:
			discovered++
		}
	}
	var parts []string
	if current > 0 {
		parts = append(parts, "● here")
	}
	if recent > 0 {
		parts = append(parts, "recent")
	}
	if discovered > 0 {
		parts = append(parts, "found on disk")
	}
	return strings.Join(parts, " · ")
}

// Rows, Highlighted, ListStatus, StatusLine and Breadcrumb are the picker's
// state as the frame would show it (SPEC §12).
func (p *RepoPicker) Rows() []SearchableItem { return p.list.Rows() }

// Highlighted is the row Enter would choose.
func (p *RepoPicker) Highlighted() (SearchableItem, bool) { return p.list.SelectedItem() }

// ListStatus is the line shown instead of rows (loading, empty, no matches).
func (p *RepoPicker) ListStatus() string { return p.list.StatusText() }

// StatusLine is the composed muted line of SPEC §8.
func (p *RepoPicker) StatusLine() string { return p.status }

// Breadcrumb is `<issue> ▸ choose a repository`.
func (p *RepoPicker) Breadcrumb() string { return p.breadcrumb }

func (p *RepoPicker) FocusInput() tea.Cmd { return p.list.FocusInput() }

func (p *RepoPicker) SetWidth(width int) { p.list.SetWidth(width) }

func (p *RepoPicker) Update(msg tea.Msg) tea.Cmd { return p.list.Update(msg) }

func (p *RepoPicker) Destroy() {}

func (p *RepoPicker) View() string {
	lines := []string{
		styleForeground.Render(p.breadcrumb),
		styleMuted.Render("type to search · ↑/↓ move · Enter select · Esc back"),
		styleMuted.Render(p.status),
		"",
		p.list.View(),
	}
	return strings.Join(lines, "\n")
}

// RankRepos groups the repositories the picker offers: the one the user is
// standing in first, then the ones they used before in recency order, then
// everything else found on disk. A repository appears once, in its best group.
func RankRepos(current *domain.Repo, recent []domain.Repo, discovered []domain.Repo) []RankedRepo {
	seen := map[string]bool{}
	var ranked []RankedRepo

	add := func(repo domain.Repo, group RepoGroup) {
		if repo.Root == "" || seen[repo.Root] {
			return
		}
		seen[repo.Root] = true
		ranked = append(ranked, RankedRepo{Repo: repo, Group: group})
	}

	if current != nil {
		add(*current, RepoGroupCurrent)
	}
	for _, repo := range recent {
		add(repo, RepoGroupRecent)
	}
	for _, repo := range discovered {
		add(repo, RepoGroupDiscovered)
	}
	return ranked
}
