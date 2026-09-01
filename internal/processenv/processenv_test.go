package processenv

import (
	"reflect"
	"testing"
)

func TestWithoutRemovesSensitiveNamesCaseInsensitively(t *testing.T) {
	got := Without([]string{
		"PATH=/bin",
		"TICKETS_TOKEN=secret",
		"tickets_token=also-secret",
		"HOME=/tmp",
	}, []string{"TICKETS_TOKEN"})
	want := []string{"PATH=/bin", "HOME=/tmp"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("environment = %q, want %q", got, want)
	}
}

func TestFromMapSortsAndRemovesSensitiveNames(t *testing.T) {
	got := FromMap(map[string]string{
		"ZED":           "last",
		"tickets_token": "secret",
		"ALPHA":         "first",
	}, []string{"TICKETS_TOKEN"})
	want := []string{"ALPHA=first", "ZED=last"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("environment = %q, want %q", got, want)
	}
}

func TestBuiltInProviderSecretsReturnsACopy(t *testing.T) {
	first := BuiltInProviderSecrets()
	first[0] = "changed"
	if BuiltInProviderSecrets()[0] == "changed" {
		t.Fatal("caller mutated the built-in secret policy")
	}
}
