package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/snaylaker/lw/internal/lwerr"
)

var testHome = map[string]string{"HOME": "/tmp/fake-home"}

func writeFile(t *testing.T, path, contents string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// asJSON renders through the same encoder the config writer uses, so a test
// expectation pins both the shape and the key order of a section.
func asJSON(t *testing.T, value any) string {
	t.Helper()
	encoded, err := MarshalCompact(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(encoded)
}

func assertJSON(t *testing.T, value any, want string) {
	t.Helper()
	if got := asJSON(t, value); got != want {
		t.Errorf("got %s, want %s", got, want)
	}
}

func mustRead(t *testing.T, path string) *StoredConfig {
	t.Helper()
	stored, err := ReadStoredConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	return stored
}

func TestReadStoredConfigReturnsNilForAMissingFile(t *testing.T) {
	stored, err := ReadStoredConfig(filepath.Join(t.TempDir(), "nonexistent", "config.json"))
	if err != nil || stored != nil {
		t.Fatalf("got (%v, %v), want (nil, nil)", stored, err)
	}
	// A path component that is a file, not a directory, is also "not configured".
	file := writeFile(t, filepath.Join(t.TempDir(), "notadir"), "x")
	stored, err = ReadStoredConfig(filepath.Join(file, "config.json"))
	if err != nil || stored != nil {
		t.Fatalf("got (%v, %v), want (nil, nil)", stored, err)
	}
}

func TestAMalformedConfigFileIsReportedInsteadOfReadAsEmpty(t *testing.T) {
	dir := t.TempDir()
	cases := []struct{ name, contents, problem string }{
		{"corrupt.json", `{"projects":{},}`, "is not valid JSON"},
		{"scalar.json", `"just a string"`, "must hold a JSON object, found a string"},
		{"array.json", `[]`, "must hold a JSON object, found an array"},
		{"number.json", `42`, "must hold a JSON object, found a number"},
		{"boolean.json", `true`, "must hold a JSON object, found a boolean"},
		{"null.json", `null`, "must hold a JSON object, found null"},
	}
	for _, tc := range cases {
		path := writeFile(t, filepath.Join(dir, tc.name), tc.contents)
		readers := map[string]func() error{
			"ReadStoredConfig": func() error { _, err := ReadStoredConfig(path); return err },
		}
		for name, read := range readers {
			err := read()
			launcher, ok := lwerr.As(err)
			if !ok {
				t.Fatalf("%s(%s): got %v, want a *lwerr.Error", name, tc.name, err)
			}
			if launcher.Kind != lwerr.ConfigInvalid {
				t.Errorf("%s(%s): kind = %q", name, tc.name, launcher.Kind)
			}
			want := "the config file " + path + " " + tc.problem
			if !strings.HasPrefix(launcher.Message, want) {
				t.Errorf("%s(%s): message = %q, want prefix %q", name, tc.name, launcher.Message, want)
			}
			if launcher.NextAction != "fix the JSON, or delete the file to start over; your stored API key is unaffected" {
				t.Errorf("%s(%s): nextAction = %q", name, tc.name, launcher.NextAction)
			}
		}
	}
}

// Section 9 prints the report itself, not just the pieces. This is what the
// user actually sees after a stray comma.
func TestAMalformedConfigFileRendersTheReportOfSection9(t *testing.T) {
	path := writeFile(t, filepath.Join(t.TempDir(), "config.json"), `{"agent":"claude",}`)
	_, err := ReadStoredConfig(path)
	var out strings.Builder
	if code := lwerr.Report(err, &out); code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	lines := strings.Split(strings.TrimSuffix(out.String(), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("report = %q, want two lines", out.String())
	}
	// SPEC §7 quotes both lines. The parse error is reachable through the
	// wrapped cause; it is not part of the message.
	if lines[0] != "error: the config file "+path+" is not valid JSON" {
		t.Errorf("first line = %q", lines[0])
	}
	if lines[1] != "next: fix the JSON, or delete the file to start over; your stored API key is unaffected" {
		t.Errorf("second line = %q", lines[1])
	}
	// The file never held a key, so nothing here may hint that it did.
	if strings.Contains(out.String(), "credentialCommand") {
		t.Errorf("report = %q, want no claim about what the file contained", out.String())
	}
}

func TestAnUnreadableConfigFileIsReported(t *testing.T) {
	// A directory where the config file belongs cannot be read as a file.
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.MkdirAll(path, 0o700); err != nil {
		t.Fatal(err)
	}
	_, err := ReadStoredConfig(path)
	launcher, ok := lwerr.As(err)
	if !ok || launcher.Kind != lwerr.ConfigInvalid {
		t.Fatalf("got %v, want a config_invalid error", err)
	}
	if !strings.HasPrefix(launcher.Message, "the config file "+path+" cannot be read: ") {
		t.Errorf("message = %q", launcher.Message)
	}
}

func TestEmptyFilesAreAbsentAndRemovedOAuthFieldsAreIgnored(t *testing.T) {
	dir := t.TempDir()
	if stored := mustRead(t, writeFile(t, filepath.Join(dir, "empty.json"), "  \n")); stored != nil {
		t.Errorf("empty file = %v, want nil", stored)
	}
	legacy := mustRead(t, writeFile(t, filepath.Join(dir, "legacy.json"), `{"clientId":"old","redirectPort":1234}`))
	if legacy == nil {
		t.Fatal("a parsed object must not read as absent")
	}
	assertJSON(t, legacy, `{}`)
}

func TestWriteIsAtomicAndUserOnly(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "config.json")
	if err := Write(&StoredConfig{WorktreeRoot: "~/w"}, path); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o700 {
		t.Errorf("directory mode = %o, want 700", got)
	}
	info, err = os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Errorf("file mode = %o, want 600", got)
	}
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "config.json" {
		t.Errorf("no temp file may survive the rename, got %v", entries)
	}
	if got, want := readFile(t, path), "{\n  \"worktreeRoot\": \"~/w\"\n}\n"; got != want {
		t.Errorf("payload = %q, want %q", got, want)
	}
}

// The writer must not HTML-escape <, > or &: config.json is read by people.
func TestWriteDoesNotHTMLEscape(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := Write(&StoredConfig{CredentialCommand: "R&D <team>"}, path); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, path); !strings.Contains(got, `"R&D <team>"`) {
		t.Errorf("payload = %q", got)
	}
}

func TestAsRecordRejectsEverythingButObjects(t *testing.T) {
	for _, raw := range []string{`null`, `[]`, `[1,2]`, `"s"`, `1`, `true`, ``} {
		if _, ok := AsRecord(json.RawMessage(raw)); ok {
			t.Errorf("AsRecord(%q) accepted", raw)
		}
	}
	record, ok := AsRecord(json.RawMessage(`{"a":1,"b":2,"a":3}`))
	if !ok {
		t.Fatal("AsRecord rejected an object")
	}
	if string(record.Get("a")) != "3" {
		t.Errorf("a = %s, want the last value", record.Get("a"))
	}
	if record.Get("missing") != nil {
		t.Error("an absent key must read as nil")
	}
}

func TestWorktreeRootSurvivesAWriteAndSitsNextToTheOtherSections(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := Write(&StoredConfig{WorktreeRoot: "~/checkouts"}, path); err != nil {
		t.Fatal(err)
	}
	if _, err := SetPruneMerged(true, path); err != nil {
		t.Fatal(err)
	}
	assertJSON(t, mustRead(t, path), `{"worktreeRoot":"~/checkouts","pruneMerged":true}`)
}

func TestCredentialCommandSurvivesAWriteAndSitsNextToTheOtherSections(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := Write(&StoredConfig{
		WorktreeRoot:      "~/checkouts",
		CredentialCommand: "op read op://private/linear/api-key",
	}, path); err != nil {
		t.Fatal(err)
	}
	if err := AddRepoRoot("~/Work", path); err != nil {
		t.Fatal(err)
	}
	assertJSON(t, mustRead(t, path), `{"repos":{"roots":["~/Work"]},`+
		`"worktreeRoot":"~/checkouts","credentialCommand":"op read op://private/linear/api-key"}`)
}

func TestSetPruneMergedReportsNoChangeWhenAlreadySet(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if _, err := SetPruneMerged(true, path); err != nil {
		t.Fatalf("SetPruneMerged: %v", err)
	}
	changed, err := SetPruneMerged(true, path)
	if err != nil {
		t.Fatalf("SetPruneMerged: %v", err)
	}
	if changed {
		t.Error("changed = true; setting the same value again changes nothing")
	}
}

// Off is the default, and it is what a missing file must answer.
func TestPruneMergedDefaultsToOff(t *testing.T) {
	enabled, err := ReadPruneMerged(filepath.Join(t.TempDir(), "absent.json"))
	if err != nil {
		t.Fatalf("ReadPruneMerged: %v", err)
	}
	if enabled {
		t.Error("enabled = true; nothing may be deleted by default")
	}
}

// The worktree root is still resolved on every run, so its rules keep their
// tests even though the standalone reader is gone.
func TestWorktreeRootDefaultsAndIsTildeExpanded(t *testing.T) {
	if got := ResolveWorktreeRoot(nil, testHome); got != "/tmp/fake-home/.lw/worktrees" {
		t.Errorf("default = %q, want the expanded DefaultWorktreeRoot", got)
	}
	if got := ResolveWorktreeRoot(&StoredConfig{WorktreeRoot: "~/checkouts"}, testHome); got != "/tmp/fake-home/checkouts" {
		t.Errorf("configured = %q, want tilde-expanded", got)
	}
	// Trimming happens on the way in, not here.
	path := writeFile(t, filepath.Join(t.TempDir(), "config.json"), `{"worktreeRoot":"  ~/checkouts  "}`)
	stored, err := ReadStoredConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if stored.WorktreeRoot != "~/checkouts" {
		t.Errorf("stored worktreeRoot = %q, want it trimmed on read", stored.WorktreeRoot)
	}
	if got := ResolveWorktreeRoot(&StoredConfig{WorktreeRoot: "/abs/path"}, testHome); got != "/abs/path" {
		t.Errorf("absolute = %q, want it left alone", got)
	}
}

// SPEC §7: an invalid entry is dropped, not fatal — the file still loads and
// every other preference survives.
func TestAnInvalidCredentialCommandIsDroppedNotFatal(t *testing.T) {
	for _, value := range []string{`42`, `true`, `null`, `[]`, `{}`, `"   "`} {
		path := writeFile(t, filepath.Join(t.TempDir(), "config.json"),
			`{"credentialCommand":`+value+`,"worktreeRoot":"~/checkouts"}`)
		stored, err := ReadStoredConfig(path)
		if err != nil {
			t.Fatalf("credentialCommand %s made the file fatal: %v", value, err)
		}
		if stored.CredentialCommand != "" {
			t.Errorf("credentialCommand %s = %q, want dropped", value, stored.CredentialCommand)
		}
		if stored.WorktreeRoot != "~/checkouts" {
			t.Errorf("credentialCommand %s took the rest of the file with it", value)
		}
	}
}

func TestCredentialCommandReadsAsWrittenAndIsTrimmed(t *testing.T) {
	path := writeFile(t, filepath.Join(t.TempDir(), "config.json"),
		`{"credentialCommand":"  op read op://private/linear/api-key  "}`)
	stored, err := ReadStoredConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if stored.CredentialCommand != "op read op://private/linear/api-key" {
		t.Errorf("credentialCommand = %q, want it trimmed", stored.CredentialCommand)
	}
}

func TestBranchNamingRulesAreRepositoryScopedAndSanitized(t *testing.T) {
	path := writeFile(t, filepath.Join(t.TempDir(), "config.json"), `{
		"branchNaming": {
			"variables": {"username": "  mehdi  "},
			"byRepository": {
				"gitlab.example.com/group/api": {"template": "  {username}/{ticket_lower}-{slug}  "},
				"broken": 42,
				"blank": {"template": "   "}
			}
		}
	}`)
	stored := mustRead(t, path)
	template, username, ok := BranchRuleFor(stored, "missing", "gitlab.example.com/group/api")
	if !ok || template != "{username}/{ticket_lower}-{slug}" || username != "mehdi" {
		t.Fatalf("rule = %q, username = %q, ok = %v", template, username, ok)
	}
	if _, _, ok := BranchRuleFor(stored, "broken", "blank"); ok {
		t.Fatal("invalid branch rules survived sanitization")
	}
}

func TestReadStoredConfigAcceptsSafeIssueProviderIDs(t *testing.T) {
	for _, provider := range []string{"linear", "github", "jira", "company_tickets"} {
		path := filepath.Join(t.TempDir(), "config.json")
		if err := os.WriteFile(path, []byte(`{"issueProvider":"`+provider+`"}`), 0o600); err != nil {
			t.Fatal(err)
		}
		stored, err := ReadStoredConfig(path)
		if err != nil || stored.IssueProvider != provider {
			t.Fatalf("provider %q: stored = %+v, err = %v", provider, stored, err)
		}
	}
}

func TestSetAndUnsetBranchRulePreserveOtherConfiguration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := Write(&StoredConfig{WorktreeRoot: "~/worktrees", PruneMerged: true}, path); err != nil {
		t.Fatal(err)
	}
	update := BranchRuleUpdate{
		Repository: "gitlab.example.com/group/api",
		Template:   "{username}/{ticket}/{slug}",
		Username:   "alex",
	}
	changed, err := SetBranchRule(update, path)
	if err != nil || !changed {
		t.Fatalf("SetBranchRule = %v, %v", changed, err)
	}
	if changed, err = SetBranchRule(update, path); err != nil || changed {
		t.Fatalf("second SetBranchRule = %v, %v", changed, err)
	}
	stored := mustRead(t, path)
	repository, template, username, ok := BranchRuleEntry(stored, "missing", update.Repository)
	if !ok || repository != update.Repository || template != update.Template || username != update.Username {
		t.Fatalf("entry = %q, %q, %q, %v", repository, template, username, ok)
	}
	if stored.WorktreeRoot != "~/worktrees" || !stored.PruneMerged {
		t.Fatalf("other configuration was lost: %+v", stored)
	}

	changed, err = UnsetBranchRule(update.Repository, path)
	if err != nil || !changed {
		t.Fatalf("UnsetBranchRule = %v, %v", changed, err)
	}
	if changed, err = UnsetBranchRule(update.Repository, path); err != nil || changed {
		t.Fatalf("second UnsetBranchRule = %v, %v", changed, err)
	}
	stored = mustRead(t, path)
	if stored.BranchNaming == nil || stored.BranchNaming.Variables.Username != "alex" || stored.BranchNaming.ByRepository != nil {
		t.Fatalf("username should survive rule removal: %+v", stored.BranchNaming)
	}
}

func TestBranchNamingSurvivesOtherConfigWrites(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	stored := &StoredConfig{BranchNaming: &BranchNaming{
		Variables:    BranchVariables{Username: "mehdi"},
		ByRepository: map[string]BranchRule{"/src/api": {Template: "{ticket_lower}-{slug}"}},
	}}
	if err := Write(stored, path); err != nil {
		t.Fatal(err)
	}
	if err := AddRepoRoot("~/Work", path); err != nil {
		t.Fatal(err)
	}
	template, username, ok := BranchRuleFor(mustRead(t, path), "/src/api")
	if !ok || template != "{ticket_lower}-{slug}" || username != "mehdi" {
		t.Fatalf("rule lost after write: %q, %q, %v", template, username, ok)
	}
}

func TestPinsAreSanitizedAndReturnedAsCopies(t *testing.T) {
	path := writeFile(t, filepath.Join(t.TempDir(), "config.json"), `{
		"pins": {
			"projects": [" project-a ", "project-a", 42, "", "project-b"],
			"teams": ["team-demo", "team-demo", null, "team-eng"]
		}
	}`)
	stored := mustRead(t, path)
	if got, want := PinnedProjects(stored), []string{"project-a", "project-b"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("project pins = %v, want %v", got, want)
	}
	if got, want := PinnedTeams(stored), []string{"team-demo", "team-eng"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("team pins = %v, want %v", got, want)
	}
	projects := PinnedProjects(stored)
	projects[0] = "mutated"
	if PinnedProjects(stored)[0] != "project-a" {
		t.Fatal("PinnedProjects exposed stored memory")
	}
}

func TestProjectAndTeamPinsTogglePersistentlyWithoutDisturbingConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := Write(&StoredConfig{WorktreeRoot: "~/worktrees"}, path); err != nil {
		t.Fatal(err)
	}
	project, err := ToggleProjectPin("project-a", path)
	if err != nil || !project.Pinned || !reflect.DeepEqual(project.IDs, []string{"project-a"}) {
		t.Fatalf("project toggle = %+v, err %v", project, err)
	}
	team, err := ToggleTeamPin("team-demo", path)
	if err != nil || !team.Pinned || !reflect.DeepEqual(team.IDs, []string{"team-demo"}) {
		t.Fatalf("team toggle = %+v, err %v", team, err)
	}
	project, err = ToggleProjectPin("project-a", path)
	if err != nil || project.Pinned || len(project.IDs) != 0 {
		t.Fatalf("project unpin = %+v, err %v", project, err)
	}
	stored := mustRead(t, path)
	if stored.WorktreeRoot != "~/worktrees" || stored.Pins == nil || len(stored.Pins.Projects) != 0 || !reflect.DeepEqual(stored.Pins.Teams, []string{"team-demo"}) {
		t.Fatalf("stored = %+v", stored)
	}
	team, err = ToggleTeamPin("team-demo", path)
	if err != nil || team.Pinned || len(team.IDs) != 0 {
		t.Fatalf("team unpin = %+v, err %v", team, err)
	}
	if stored = mustRead(t, path); stored.Pins != nil {
		t.Fatalf("empty pins section survived: %+v", stored.Pins)
	}
}

func TestRepoPreferencesAreResolvedAndInvalidRecentsAreDropped(t *testing.T) {
	path := writeFile(t, filepath.Join(t.TempDir(), "config.json"), `{
		"repos": {
			"roots": ["~/Personal"],
			"recent": [
				{"path": "~/Personal/api", "usedAt": 20},
				{"path": "~/Personal/missing-time"}
			]
		}
	}`)
	stored, err := ReadStoredConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := RepoRoots(stored, testHome); !reflect.DeepEqual(got, []string{"/tmp/fake-home/Personal"}) {
		t.Errorf("roots = %v", got)
	}
	if got := RecentRepos(stored); len(got) != 1 || got[0].Path != "~/Personal/api" {
		t.Errorf("recent = %+v, want only the valid entry", got)
	}
}

func TestAddRepoRootPreservesRecentsAndDoesNotDuplicate(t *testing.T) {
	path := writeFile(t, filepath.Join(t.TempDir(), "config.json"), `{
		"repos": {
			"roots": ["/old-root"],
			"recent": [{"path": "/old/repo", "usedAt": 1}]
		}
	}`)
	if err := AddRepoRoot("/new-root", path); err != nil {
		t.Fatal(err)
	}
	if err := AddRepoRoot("/new-root", path); err != nil {
		t.Fatal(err)
	}
	stored, err := ReadStoredConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := RepoRoots(stored, testHome); !reflect.DeepEqual(got, []string{"/old-root", "/new-root"}) {
		t.Errorf("roots = %v", got)
	}
	if got := RecentRepos(stored); len(got) != 1 || got[0].Path != "/old/repo" {
		t.Errorf("recent = %+v", got)
	}
}

func TestRecordRepoUseKeepsRootsAndMovesThePickFirst(t *testing.T) {
	path := writeFile(t, filepath.Join(t.TempDir(), "config.json"), `{
		"repos": {
			"roots": ["~/Personal"],
			"recent": [{"path": "/old", "usedAt": 1}]
		}
	}`)
	recent, err := RecordRepoUse(RepoUse{ProjectID: "project-1", Path: "/new"}, path, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(recent) != 2 || recent[0].Path != "/new" || recent[1].Path != "/old" {
		t.Errorf("recent = %+v", recent)
	}
	stored, err := ReadStoredConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := RepoRoots(stored, testHome); !reflect.DeepEqual(got, []string{"/tmp/fake-home/Personal"}) {
		t.Errorf("recording a pick lost roots: %v", got)
	}
	if got := ProjectRepoPath(stored, "project-1"); got != "/new" {
		t.Errorf("project repository = %q, want /new", got)
	}
}

func TestRecordRepoUseFallsBackToTeamOnlyForProjectlessIssues(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if _, err := RecordRepoUse(RepoUse{TeamID: "team-demo", Path: "/incidents"}, path, 1); err != nil {
		t.Fatal(err)
	}
	stored, err := ReadStoredConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := TeamRepoPath(stored, "team-demo"); got != "/incidents" {
		t.Errorf("team repository = %q", got)
	}

	if _, err := RecordRepoUse(RepoUse{ProjectID: "project-cli", TeamID: "team-demo", Path: "/cli"}, path, 2); err != nil {
		t.Fatal(err)
	}
	stored, err = ReadStoredConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := ProjectRepoPath(stored, "project-cli"); got != "/cli" {
		t.Errorf("project repository = %q", got)
	}
	if got := TeamRepoPath(stored, "team-demo"); got != "/incidents" {
		t.Errorf("project choice overwrote team fallback: %q", got)
	}
}

func TestProjectRepoAssociationsAreSanitizedAndNewestWins(t *testing.T) {
	path := writeFile(t, filepath.Join(t.TempDir(), "config.json"), `{
		"repos": {"projects": [
			{"projectId":"p1","path":"/old","usedAt":1},
			{"projectId":"p1","path":"/new","usedAt":2},
			{"projectId":"p2","path":"/other","usedAt":3},
			{"projectId":"missing-path","usedAt":4},
			"wrong-shape"
		]}
	}`)
	stored, err := ReadStoredConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := ProjectRepoPath(stored, "p1"); got != "/new" {
		t.Errorf("p1 repository = %q, want /new", got)
	}
	if got := ProjectRepoPath(stored, "p2"); got != "/other" {
		t.Errorf("p2 repository = %q, want /other", got)
	}
	if got := ProjectRepoPath(stored, "missing-path"); got != "" {
		t.Errorf("invalid association survived as %q", got)
	}
}
