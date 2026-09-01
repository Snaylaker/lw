// Package linear adapts the existing Linear client to the public provider
// contract. Linear's richer project and team browsers remain separate optional
// capabilities in the CLI.
package linear

import (
	"context"
	"fmt"
	"regexp"

	"github.com/snaylaker/lw/internal/domain"
	linearapi "github.com/snaylaker/lw/internal/linear"
	issueprovider "github.com/snaylaker/lw/provider"
)

var referenceRE = regexp.MustCompile(`^[A-Za-z0-9]+-[0-9]+$`)

var _ issueprovider.Provider = Client{}

type Client struct {
	Credential domain.Credential
	HTTPClient linearapi.Doer
}

func (Client) ID() issueprovider.ID { return issueprovider.Linear }
func (Client) DisplayName() string  { return "Linear" }
func (Client) ValidateReference(value string) error {
	if !referenceRE.MatchString(value) {
		return fmt.Errorf("expected an identifier like ENG-3971")
	}
	return nil
}

func (c Client) Resolve(ctx context.Context, reference string) (issueprovider.WorkItem, error) {
	issue, err := linearapi.ResolveIssue(ctx, linearapi.ResolveIssueRequest{
		Credential: c.Credential, Identifier: reference, HTTPClient: c.HTTPClient,
	})
	if err != nil {
		return issueprovider.WorkItem{}, err
	}
	return toWorkItem(issue), nil
}

func (c Client) Search(ctx context.Context, query string) ([]issueprovider.WorkItem, error) {
	issues, err := linearapi.FindIssues(ctx, linearapi.FindIssuesRequest{
		Credential: c.Credential, Query: query, HTTPClient: c.HTTPClient,
	})
	if err != nil {
		return nil, err
	}
	items := make([]issueprovider.WorkItem, 0, len(issues))
	for _, issue := range issues {
		items = append(items, toWorkItem(issue))
	}
	return items, nil
}

func toWorkItem(issue domain.Issue) issueprovider.WorkItem {
	item := issueprovider.WorkItem{
		Provider: issueprovider.Linear, ExternalID: issue.ID,
		Reference: issue.Identifier, WorktreeKey: issue.Identifier,
		Title: issue.Title, URL: issue.URL, StateType: issue.StateType,
		StateName: issue.StateName, SuggestedBranch: issue.SuggestedBranch,
		BranchKeys: []string{issue.Identifier},
	}
	if issue.ProjectID != "" {
		item.Scopes = append(item.Scopes, issueprovider.Scope{
			Kind: "linear_project", ID: issue.ProjectID, Name: issue.ProjectName,
		})
	}
	if issue.TeamID != "" {
		item.Scopes = append(item.Scopes, issueprovider.Scope{
			Kind: "linear_team", ID: issue.TeamID, Key: issue.TeamKey, Name: issue.TeamName,
		})
	}
	return item
}
