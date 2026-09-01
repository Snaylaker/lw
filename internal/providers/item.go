// Package providers adapts the public provider contract to lw's internal
// domain while the Linear-only project and team browsers remain compatible.
package providers

import (
	"strings"

	"github.com/snaylaker/lw/internal/domain"
	issueprovider "github.com/snaylaker/lw/provider"
)

func ToDomain(item issueprovider.WorkItem) domain.Issue {
	issue := domain.Issue{
		ID:              item.ExternalID,
		Provider:        string(item.Provider),
		ExternalID:      item.ExternalID,
		Identifier:      item.WorktreeKey,
		Reference:       item.Reference,
		Title:           item.Title,
		URL:             item.URL,
		StateType:       item.StateType,
		StateName:       item.StateName,
		SuggestedBranch: item.SuggestedBranch,
		BranchKeys:      append([]string(nil), item.BranchKeys...),
	}
	if issue.Reference == "" {
		issue.Reference = issue.Identifier
	}
	if issue.ID == "" {
		issue.ID = string(item.Provider) + ":" + issue.Reference
	}
	for _, scope := range item.Scopes {
		switch scope.Kind {
		case "linear_project":
			issue.ProjectID, issue.ProjectName = scope.ID, scope.Name
		case "linear_team":
			issue.TeamID, issue.TeamKey, issue.TeamName = scope.ID, scope.Key, scope.Name
		default:
			// Existing repository routing stores a most-specific scope in its
			// project slot. Prefixing it prevents IDs from different providers
			// or collection kinds from colliding.
			if issue.ProjectID == "" && strings.TrimSpace(scope.ID) != "" {
				issue.ProjectID = string(item.Provider) + ":" + scope.Kind + ":" + scope.ID
				issue.ProjectName = scope.Name
				if issue.ProjectName == "" {
					issue.ProjectName = scope.Key
				}
			}
		}
	}
	return issue
}

func ToDomains(items []issueprovider.WorkItem) []domain.Issue {
	result := make([]domain.Issue, 0, len(items))
	for _, item := range items {
		result = append(result, ToDomain(item))
	}
	return result
}
