// Command screenshots renders deterministic, synthetic lw screens for the
// project documentation. Pipe its ANSI output into Freeze to create images.
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/snaylaker/lw/internal/domain"
	"github.com/snaylaker/lw/internal/tui"
)

func main() {
	screen := flag.String("screen", "provider-search", "provider-search, branch-name, or worktree-ready")
	flag.Parse()

	var frame string
	switch *screen {
	case "provider-search":
		frame = providerSearch()
	case "branch-name":
		frame = branchName()
	case "worktree-ready":
		frame = worktreeReady()
	default:
		fmt.Fprintf(os.Stderr, "unknown screen %q\n", *screen)
		os.Exit(2)
	}
	fmt.Println(frame)
}

func providerSearch() string {
	picker := tui.NewIssuePicker(tui.IssuePickerOptions{ProviderName: "GitHub"})
	picker.SetWidth(76)
	picker.SetQuery("cache")
	picker.SetIssues([]domain.Issue{
		{
			ID: "github-248", Provider: "github", Identifier: "GH-acme-platform-248",
			Reference: "acme/platform#248", Title: "Repair cache invalidation after deploy",
			StateName: "open", ProjectName: "acme/platform",
		},
		{
			ID: "github-231", Provider: "github", Identifier: "GH-acme-platform-231",
			Reference: "acme/platform#231", Title: "Cache repository metadata between commands",
			StateName: "open", ProjectName: "acme/platform",
		},
		{
			ID: "github-219", Provider: "github", Identifier: "GH-acme-cli-219",
			Reference: "acme/cli#219", Title: "Document cache cleanup behavior",
			StateName: "open", ProjectName: "acme/cli",
		},
	})
	_ = picker.FocusInput()
	return picker.View()
}

func branchName() string {
	issue := domain.Issue{
		Provider: "jira", Identifier: "OPS-842", Reference: "OPS-842",
		Title: "Repair cache invalidation after deploy",
	}
	repo := domain.Repo{Root: "/Users/alex/Work/platform-api", Name: "platform-api"}
	input := tui.NewBranchInput(issue, repo, "alex/ops-842-repair-cache-invalidation", nil)
	return input.View()
}

func worktreeReady() string {
	view := tui.NewProgressView()
	view.ApplyUpdate(domain.StageUpdate{Stage: domain.StagePreparing, State: domain.StateDone})
	view.ApplyUpdate(domain.StageUpdate{Stage: domain.StageCreatingWorktree, State: domain.StateDone})
	view.ShowResult(domain.FlowResult{
		CheckoutPath: "/Users/alex/.lw/worktrees/platform-api/OPS-842",
		Created:      true,
	})
	return view.View()
}
