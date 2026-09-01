package provider

import "testing"

func TestWorkItemUsesHumanReferenceAndDurableScopes(t *testing.T) {
	item := WorkItem{
		Reference:   "acme/api#42",
		WorktreeKey: "GH-acme-api-42",
		Scopes:      []Scope{{Kind: "github_repository", ID: "acme/api", Key: "api", Name: "acme/api"}},
	}
	if got := item.DisplayReference(); got != "acme/api#42" {
		t.Fatalf("reference = %q", got)
	}
	scope, ok := item.Scope("github_repository")
	if !ok || scope.ID != "acme/api" || item.ScopeLabel() != "acme/api" {
		t.Fatalf("scope = %+v, %v", scope, ok)
	}
}

func TestWorkItemFallsBackToItsSafeKey(t *testing.T) {
	item := WorkItem{WorktreeKey: "OPS-42"}
	if got := item.DisplayReference(); got != "OPS-42" {
		t.Fatalf("reference = %q", got)
	}
	if _, ok := item.Scope("missing"); ok || item.ScopeLabel() != "" {
		t.Fatal("missing scope was reported")
	}
}
