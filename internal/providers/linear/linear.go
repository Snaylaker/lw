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
	return linearapi.ResolveIssue(ctx, linearapi.ResolveIssueRequest{
		Credential: c.Credential, Identifier: reference, HTTPClient: c.HTTPClient,
	})
}

func (c Client) Search(ctx context.Context, query string) ([]issueprovider.WorkItem, error) {
	return linearapi.FindIssues(ctx, linearapi.FindIssuesRequest{
		Credential: c.Credential, Query: query, HTTPClient: c.HTTPClient,
	})
}
