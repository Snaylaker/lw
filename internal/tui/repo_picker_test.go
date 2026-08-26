package tui

import (
	"testing"

	"github.com/snaylaker/lw/internal/domain"
)

var (
	here      = domain.Repo{Root: "/repos/here", Name: "here"}
	usedOnce  = domain.Repo{Root: "/repos/used", Name: "used"}
	onDisk    = domain.Repo{Root: "/repos/found", Name: "found"}
	otherDisk = domain.Repo{Root: "/repos/other", Name: "other"}
)

// SPEC §4 fixes the order: where you are, then what you used, then what was found.
func TestRankReposGroupsCurrentThenRecentThenDiscovered(t *testing.T) {
	ranked := RankRepos(&here, []domain.Repo{usedOnce}, []domain.Repo{onDisk, otherDisk})

	want := []struct {
		name  string
		group RepoGroup
	}{
		{"here", RepoGroupCurrent},
		{"used", RepoGroupRecent},
		{"found", RepoGroupDiscovered},
		{"other", RepoGroupDiscovered},
	}
	if len(ranked) != len(want) {
		t.Fatalf("ranked = %+v, want %d rows", ranked, len(want))
	}
	for i, expected := range want {
		if ranked[i].Repo.Name != expected.name || ranked[i].Group != expected.group {
			t.Errorf("row %d = %+v, want %s/%s", i, ranked[i], expected.name, expected.group)
		}
	}
}

// A repository appears once, in its best group — being recent must not hide the
// fact that it is also the one you are standing in.
func TestRankReposKeepsARepositoryOnlyInItsBestGroup(t *testing.T) {
	ranked := RankRepos(&here, []domain.Repo{here, usedOnce}, []domain.Repo{here, usedOnce})

	if len(ranked) != 2 {
		t.Fatalf("ranked = %+v, want two rows", ranked)
	}
	if ranked[0].Repo.Root != here.Root || ranked[0].Group != RepoGroupCurrent {
		t.Errorf("first row = %+v, want the current repository", ranked[0])
	}
	if ranked[1].Group != RepoGroupRecent {
		t.Errorf("second row = %+v, want the recent group", ranked[1])
	}
}

// Outside a repository there is no current row, and the picker still opens.
func TestRankReposWithoutACurrentRepository(t *testing.T) {
	ranked := RankRepos(nil, nil, []domain.Repo{onDisk})
	if len(ranked) != 1 || ranked[0].Group != RepoGroupDiscovered {
		t.Fatalf("ranked = %+v, want the discovered repository alone", ranked)
	}
}

func TestRepoPickerRowsCarryTheMarkerAndThePath(t *testing.T) {
	picker := NewRepoPicker(RepoPickerOptions{Context: "DEMO-4009"})
	picker.SetRepos(RankRepos(&here, nil, []domain.Repo{onDisk}))

	rows := picker.Rows()
	if len(rows) != 2 {
		t.Fatalf("rows = %+v", rows)
	}
	if rows[0].Label != CurrentMarker+"here" {
		t.Errorf("first label = %q, want the current marker", rows[0].Label)
	}
	if rows[0].Hint != "/repos/here" {
		t.Errorf("first hint = %q, want the path", rows[0].Hint)
	}
	if rows[1].Label != "found" {
		t.Errorf("second label = %q, want no marker", rows[1].Label)
	}
	if picker.Breadcrumb() != "DEMO-4009 ▸ choose a repository" {
		t.Errorf("breadcrumb = %q", picker.Breadcrumb())
	}
}
