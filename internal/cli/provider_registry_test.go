package cli

import (
	"context"
	"reflect"
	"strings"
	"testing"

	issueprovider "github.com/snaylaker/lw/provider"
)

type registryTestProvider struct {
	id        issueprovider.ID
	sensitive []string
}

func (p registryTestProvider) ID() issueprovider.ID           { return p.id }
func (p registryTestProvider) DisplayName() string            { return strings.ToUpper(string(p.id)) }
func (p registryTestProvider) ValidateReference(string) error { return nil }
func (p registryTestProvider) Resolve(context.Context, string) (issueprovider.WorkItem, error) {
	return issueprovider.WorkItem{}, nil
}
func (p registryTestProvider) Search(context.Context, string) ([]issueprovider.WorkItem, error) {
	return nil, nil
}
func (p registryTestProvider) SensitiveEnvironmentVariables() []string {
	return p.sensitive
}

func TestProviderPrefixAcceptsCanonicalCustomIDs(t *testing.T) {
	id, reference, ok := prefixedProviderReference("tickets:T-42")
	if !ok || id != "tickets" || reference != "T-42" {
		t.Fatalf("prefix = %q, %q, %v", id, reference, ok)
	}
	if _, _, ok := prefixedProviderReference("Tickets!:T-42"); ok {
		t.Fatal("invalid provider prefix was accepted")
	}
}

func TestProviderRegistryRejectsAmbiguousExtensionIDs(t *testing.T) {
	for _, tc := range []struct {
		name       string
		providers  []issueprovider.Provider
		wantDetail string
	}{
		{name: "nil", providers: []issueprovider.Provider{nil}, wantDetail: "is nil"},
		{name: "uppercase", providers: []issueprovider.Provider{registryTestProvider{id: "Tickets"}}, wantDetail: "must use lowercase"},
		{name: "reserved", providers: []issueprovider.Provider{registryTestProvider{id: issueprovider.Linear}}, wantDetail: "reserved"},
		{name: "duplicate", providers: []issueprovider.Provider{registryTestProvider{id: "tickets"}, registryTestProvider{id: "tickets"}}, wantDetail: "more than once"},
		{name: "bad secret", providers: []issueprovider.Provider{registryTestProvider{id: "tickets", sensitive: []string{"NOT=AN_ENV_NAME"}}}, wantDetail: "invalid sensitive"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := newProviderRegistry(tc.providers)
			if err == nil || !strings.Contains(err.Error(), tc.wantDetail) {
				t.Fatalf("error = %v, want detail %q", err, tc.wantDetail)
			}
		})
	}
}

func TestProviderRegistryCombinesBuiltInAndExtensionSecrets(t *testing.T) {
	registry, err := newProviderRegistry([]issueprovider.Provider{
		registryTestProvider{id: "tickets", sensitive: []string{"TICKETS_TOKEN"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"GH_TOKEN", "GITHUB_TOKEN", "JIRA_API_TOKEN", "LINEAR_API_KEY", "TICKETS_TOKEN"}
	if got := registry.sensitiveNames(); !reflect.DeepEqual(got, want) {
		t.Fatalf("sensitive names = %q, want %q", got, want)
	}
	if registry.displayName("tickets") != "TICKETS" {
		t.Fatalf("display name = %q", registry.displayName("tickets"))
	}
}
