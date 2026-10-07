package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// Before any report the tab says so and opens the setup steps; the steps
// carry the hook pointed at this installation's binary.
func TestClaudeTabBeforeAnyReport(t *testing.T) {
	t.Setenv("BRANCHKIT_PLUGIN_DIR", "/Users/x/Library/Application Support/BranchKit/plugins/address")
	h := newHost(newFakePlatform(), testLex)
	html, err := h.renderClaudeTab(nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"Not set up",
		`<details class="page-more address-setup" open>`,
		"~/.claude/settings.json",
		`Application Support/BranchKit/plugins/address/`,
		"claude-hook",
		"UserPromptSubmit",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("tab missing %q", want)
		}
	}
	if strings.Contains(html, "Project</div>") {
		t.Error("no project row before a project is known")
	}
}

// Names loaded from before a restart: the hook worked once, so the steps
// stay closed, and the tab says the names wait for the next prompt.
func TestClaudeTabWithNamesFromAnEarlierSession(t *testing.T) {
	f := newFakePlatform()
	h := newHost(f, testLex)
	if err := h.publish(projectReport{Cwd: testCwd, Files: []string{"docs/MISSION.md"}}); err != nil {
		t.Fatal(err)
	}
	h2 := newHost(f, testLex)
	if err := h2.loadProject(); err != nil {
		t.Fatal(err)
	}
	html, err := h2.renderClaudeTab(nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Waiting", testCwd, "1 name for 1 file"} {
		if !strings.Contains(html, want) {
			t.Errorf("tab missing %q", want)
		}
	}
	if strings.Contains(html, "address-setup\" open") {
		t.Error("the steps should be closed once the hook has worked")
	}
}

// A report marks the hook connected and redraws an open tab.
func TestAReportConnectsTheTab(t *testing.T) {
	f := newFakePlatform()
	h := newHost(f, testLex)
	body, _ := json.Marshal(projectReport{Cwd: testCwd, Files: []string{"docs/MISSION.md", "docs/DEV_LOOP.md"}})
	rec := httptest.NewRecorder()
	h.handleProject(rec, httptest.NewRequest(http.MethodPost, "/project", bytes.NewReader(body)))
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	deadline := time.Now().Add(2 * time.Second)
	for !f.has(filesCollection, "mission") {
		if time.Now().After(deadline) {
			t.Fatal("the report was never published")
		}
		time.Sleep(5 * time.Millisecond)
	}
	html, err := h.renderClaudeTab(nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Connected", "Claude Code reported this project at", testCwd, "names for 2 files"} {
		if !strings.Contains(html, want) {
			t.Errorf("tab missing %q", want)
		}
	}
	f.mu.Lock()
	refreshes := f.refreshes
	f.mu.Unlock()
	if refreshes == 0 {
		t.Error("a report should redraw the tab")
	}
}

// The command survives the shell: the folder's space stays inside the
// quotes, and characters the shell would expand are escaped.
func TestHookCommandQuotesThePath(t *testing.T) {
	for in, want := range map[string]string{
		"/Users/x/Library/Application Support/BranchKit/plugins/address/address-plugin": `"/Users/x/Library/Application Support/BranchKit/plugins/address/address-plugin" claude-hook`,
		"/tmp/a$b`c\"d\\e/address-plugin":                                               `"/tmp/a\$b` + "\\`" + `c\"d\\e/address-plugin" claude-hook`,
	} {
		if got := hookCommand(in); got != want {
			t.Errorf("hookCommand(%q) = %s, want %s", in, got, want)
		}
	}
}

// The block is valid JSON in the shape Claude Code reads, the same hook on
// both events.
func TestHookSettingsShape(t *testing.T) {
	cmd := hookCommand("/p/address-plugin")
	raw, err := hookSettings(cmd)
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Hooks map[string][]struct {
			Hooks []struct {
				Type    string `json:"type"`
				Command string `json:"command"`
				Timeout int    `json:"timeout"`
			} `json:"hooks"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, raw)
	}
	for _, event := range []string{"SessionStart", "UserPromptSubmit"} {
		m := got.Hooks[event]
		if len(m) != 1 || len(m[0].Hooks) != 1 || m[0].Hooks[0].Command != cmd || m[0].Hooks[0].Type != "command" {
			t.Errorf("%s = %+v", event, m)
		}
	}
}

func TestHeardAt(t *testing.T) {
	now := time.Date(2026, 10, 7, 18, 0, 0, 0, time.Local)
	if got := heardAt(now.Add(-time.Hour), now); got != "5:00 PM" {
		t.Errorf("today: %q", got)
	}
	if got := heardAt(now.Add(-48*time.Hour), now); got != "Oct 5, 6:00 PM" {
		t.Errorf("earlier: %q", got)
	}
}
