package jira

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestResolveAndSearchJiraIssues(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		wantAuth := "Basic " + base64.StdEncoding.EncodeToString([]byte("alex@example.com:token_test"))
		if r.Header.Get("Authorization") != wantAuth {
			t.Errorf("authorization = %q", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		issue := `{"id":"10042","key":"OPS-42","fields":{"summary":"Repair cache invalidation","status":{"name":"In Progress","statusCategory":{"key":"indeterminate"}},"project":{"id":"10000","key":"OPS","name":"Operations"}}}`
		switch r.URL.Path {
		case "/rest/api/3/issue/OPS-42":
			_, _ = w.Write([]byte(issue))
		case "/rest/api/3/search/jql":
			var body struct {
				JQL string `json:"jql"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(body.JQL, `text ~ "cache"`) {
				t.Errorf("jql = %q", body.JQL)
			}
			_, _ = w.Write([]byte(`{"issues":[` + issue + `],"isLast":true}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client, err := New(Options{
		BaseURL: server.URL, Email: "alex@example.com", Token: "token_test",
	})
	if err != nil {
		t.Fatal(err)
	}
	item, err := client.Resolve(t.Context(), "ops-42")
	if err != nil {
		t.Fatal(err)
	}
	if item.Reference != "OPS-42" || item.WorktreeKey != "OPS-42" || item.StateType != "started" || item.Provider != "jira" {
		t.Fatalf("resolved item = %+v", item)
	}
	items, err := client.Search(t.Context(), "cache")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].URL != server.URL+"/browse/OPS-42" {
		t.Fatalf("search items = %+v", items)
	}
}

func TestJiraRequiresHTTPSAwayFromLocalhostAndCredentials(t *testing.T) {
	if _, err := New(Options{BaseURL: "http://jira.example.com", Email: "a", Token: "b"}); err == nil {
		t.Fatal("insecure remote Jira URL was accepted")
	}
	if _, err := New(Options{BaseURL: "https://jira.example.com"}); err == nil {
		t.Fatal("missing credentials were accepted")
	}
}
