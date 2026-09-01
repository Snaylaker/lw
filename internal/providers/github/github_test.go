package github

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestResolveAndSearchGitHubIssues(t *testing.T) {
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.RequestURI())
		if r.Header.Get("Authorization") != "Bearer github_test" {
			t.Errorf("authorization = %q", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/repos/acme/api/issues/42":
			_, _ = w.Write([]byte(`{"id":42,"node_id":"I_kw42","number":42,"title":"Repair cache invalidation","html_url":"https://github.com/acme/api/issues/42","state":"open"}`))
		case "/search/issues":
			if query := r.URL.Query().Get("q"); !strings.Contains(query, "cache is:issue is:open repo:acme/api") {
				t.Errorf("search query = %q", query)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"items": []any{map[string]any{
				"id": 42, "node_id": "I_kw42", "number": 42,
				"title": "Repair cache invalidation", "html_url": "https://github.com/acme/api/issues/42",
				"state": "open", "repository_url": serverURL(r) + "/repos/acme/api",
			}}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client, err := New(Options{Token: "github_test", APIURL: server.URL, Repository: "acme/api"})
	if err != nil {
		t.Fatal(err)
	}
	item, err := client.Resolve(t.Context(), "#42")
	if err != nil {
		t.Fatal(err)
	}
	if item.Reference != "acme/api#42" || item.WorktreeKey != "GH-acme-api-42" || item.Provider != "github" {
		t.Fatalf("resolved item = %+v", item)
	}
	items, err := client.Search(t.Context(), "cache")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Reference != "acme/api#42" {
		t.Fatalf("search items = %+v", items)
	}
	if len(paths) != 2 {
		t.Fatalf("paths = %v", paths)
	}
}

func TestGitHubReferenceNeedsRepositoryContextForAShortNumber(t *testing.T) {
	client, err := New(Options{})
	if err != nil {
		t.Fatal(err)
	}
	if err := client.ValidateReference("#42"); err == nil {
		t.Fatal("short reference was accepted without a repository")
	}
	if err := client.ValidateReference("acme/api#42"); err != nil {
		t.Fatal(err)
	}
}

func TestGitHubRejectsAnInsecureRemoteAPIURL(t *testing.T) {
	if _, err := New(Options{APIURL: "http://github.example.com", Token: "secret"}); err == nil {
		t.Fatal("insecure remote GitHub API URL was accepted")
	}
}

func serverURL(r *http.Request) string {
	return "http://" + r.Host
}
