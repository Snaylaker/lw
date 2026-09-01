package lw_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	lw "github.com/snaylaker/lw"
	issueprovider "github.com/snaylaker/lw/provider"
)

type extension struct{}

func (extension) ID() issueprovider.ID           { return "tickets" }
func (extension) DisplayName() string            { return "Tickets" }
func (extension) ValidateReference(string) error { return nil }
func (extension) Resolve(context.Context, string) (issueprovider.WorkItem, error) {
	return issueprovider.WorkItem{}, nil
}
func (extension) Search(context.Context, string) ([]issueprovider.WorkItem, error) {
	return nil, nil
}

func TestPublicRunAcceptsProviderExtensions(t *testing.T) {
	var stdout bytes.Buffer
	code := lw.Run([]string{"--help"}, lw.Options{
		Stdout: &stdout, Providers: []issueprovider.Provider{extension{}},
	})
	if code != 0 || !strings.Contains(stdout.String(), "--provider") {
		t.Fatalf("code = %d, stdout = %q", code, stdout.String())
	}
}
