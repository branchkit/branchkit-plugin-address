package main

import (
	"bytes"
	"encoding/json"
	"html/template"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Before any report the tab says so and opens the setup steps; the steps
// carry the hook pointed at this installation's binary.
func TestClaudeTabBeforeAnyReport(t *testing.T) {
	dir := filepath.FromSlash("/tmp/a folder/plugins/address")
	t.Setenv("BRANCHKIT_PLUGIN_DIR", dir)
	h := newHost(newFakePlatform(), testLex)
	html, err := h.renderClaudeTab(nil)
	if err != nil {
		t.Fatal(err)
	}
	// The folder as it appears in the snippet: this OS's separators,
	// JSON-escaped (a Windows backslash is doubled), then HTML-escaped.
	quoted, err := json.Marshal(dir + string(filepath.Separator))
	if err != nil {
		t.Fatal(err)
	}
	folder := template.HTMLEscapeString(strings.Trim(string(quoted), `"`))
	for _, want := range []string{
		"Not set up",
		`<details class="page-more address-setup" open>`,
		"~/.claude/settings.json",
		folder,
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
	for _, want := range []string{"Connected", "Claude Code reported this project at formatted 20", testCwd, "names for 2 files"} {
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

// The block is valid JSON in the shape Claude Code reads, the same hook on
// both events, in exec form: the binary's path exactly as given, whatever
// a shell would make of it (a space, a Windows path's backslashes, `$`,
// a backtick, a quote), and the subcommand as its own argument.
func TestHookSettingsShape(t *testing.T) {
	for _, binary := range []string{
		"/p/address-plugin",
		"/tmp/a folder/plugins/address/address-plugin",
		`C:\Users\a b\plugins\address\address-plugin.exe`,
		"/tmp/a$b`c\"d\\e/address-plugin",
	} {
		raw, err := hookSettings(binary)
		if err != nil {
			t.Fatal(err)
		}
		var got struct {
			Hooks map[string][]struct {
				Hooks []struct {
					Type    string   `json:"type"`
					Command string   `json:"command"`
					Args    []string `json:"args"`
					Timeout int      `json:"timeout"`
				} `json:"hooks"`
			} `json:"hooks"`
		}
		if err := json.Unmarshal([]byte(raw), &got); err != nil {
			t.Fatalf("not JSON: %v\n%s", err, raw)
		}
		for _, event := range []string{"SessionStart", "UserPromptSubmit"} {
			m := got.Hooks[event]
			if len(m) != 1 || len(m[0].Hooks) != 1 {
				t.Fatalf("%s = %+v", event, m)
			}
			h := m[0].Hooks[0]
			if h.Type != "command" || h.Command != binary || len(h.Args) != 1 || h.Args[0] != "claude-hook" {
				t.Errorf("%s for %q = %+v", event, binary, h)
			}
		}
	}
}

// The binary's path is the plugin folder joined with the binary's own name
// by this OS's rules, so on Windows it is a Windows path.
func TestPluginBinaryIsInThePluginFolder(t *testing.T) {
	dir := filepath.FromSlash("/tmp/a folder/plugins/address")
	t.Setenv("BRANCHKIT_PLUGIN_DIR", dir)
	exe, err := os.Executable()
	if err != nil {
		t.Skip(err)
	}
	if got, want := pluginBinary(), filepath.Join(dir, filepath.Base(exe)); got != want {
		t.Errorf("pluginBinary() = %q, want %q", got, want)
	}
}
