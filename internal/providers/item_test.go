package providers

import (
	"strings"
	"testing"

	issueprovider "github.com/snaylaker/lw/provider"
)

func TestNormalizeProviderItem(t *testing.T) {
	issue, err := Normalize("tickets", issueprovider.WorkItem{
		ExternalID: " 42 ", WorktreeKey: "T-42", Title: " Repair cache ",
		BranchKeys: []string{" T-42 ", "T-42", "cache"},
		Scopes: []issueprovider.Scope{
			{Kind: " project ", ID: " platform ", Key: "PLAT"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if issue.Provider != "tickets" || issue.WorktreeKey != "T-42" || issue.Reference != "T-42" || issue.ExternalID != "42" {
		t.Fatalf("issue = %+v", issue)
	}
	if len(issue.BranchKeys) != 2 || len(issue.Scopes) != 1 || issue.Scopes[0].ID != "platform" || issue.Scopes[0].Key != "PLAT" {
		t.Fatalf("issue = %+v", issue)
	}
}

func TestNormalizeRejectsContradictoryOrUnsafeProviderItems(t *testing.T) {
	for _, tc := range []struct {
		name string
		item issueprovider.WorkItem
		want string
	}{
		{name: "provider mismatch", item: issueprovider.WorkItem{Provider: "other", WorktreeKey: "T-42"}, want: "returned provider"},
		{name: "separator", item: issueprovider.WorkItem{WorktreeKey: "owner/repo#42"}, want: "unsafe worktree key"},
		{name: "windows device", item: issueprovider.WorkItem{WorktreeKey: "CON.txt"}, want: "unsafe worktree key"},
		{name: "control", item: issueprovider.WorkItem{WorktreeKey: "T-42\n"}, want: "worktree key"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Normalize("tickets", tc.item)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestNormalizeAllFailsTheWholeResponseWhenOneItemViolatesTheContract(t *testing.T) {
	_, err := NormalizeAll("tickets", []issueprovider.WorkItem{
		{WorktreeKey: "T-1"},
		{WorktreeKey: "../escape"},
	})
	if err == nil {
		t.Fatal("invalid provider result was accepted")
	}
}
