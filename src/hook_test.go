package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/branchkit/plugin-sdk-go"
)

// gitRepo makes a repository with committed, modified and new files, and a
// subfolder to run from.
func gitRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	write := func(p, s string) {
		t.Helper()
		full := filepath.Join(dir, p)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	run("init", "-q")
	write("README.md", "x")
	write("docs/MISSION.md", "x")
	write("docs/DEV_LOOP.md", "x")
	write(".gitignore", "build/\n")
	run("add", ".")
	run("commit", "-q", "-m", "init")
	write("docs/MISSION.md", "changed")
	write("docs/NEW.md", "new")
	write("build/out.bin", "ignored")
	return dir
}

func TestListProjectListsFilesRelativeToTheFolder(t *testing.T) {
	dir := gitRepo(t)
	rep, err := listProject(context.Background(), filepath.Join(dir, "docs"))
	if err != nil || rep == nil {
		t.Fatalf("rep %v, err %v", rep, err)
	}
	files := append([]string(nil), rep.Files...)
	sort.Strings(files)
	if want := []string{"DEV_LOOP.md", "MISSION.md", "NEW.md"}; !reflect.DeepEqual(files, want) {
		t.Errorf("files = %q, want %q (relative to docs/, ignored files left out)", files, want)
	}
	changed := append([]string(nil), rep.Changed...)
	sort.Strings(changed)
	if want := []string{"MISSION.md", "NEW.md"}; !reflect.DeepEqual(changed, want) {
		t.Errorf("changed = %q, want %q", changed, want)
	}
}

func TestListProjectOutsideGitIsNothing(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	rep, err := listProject(context.Background(), t.TempDir())
	if rep != nil || err != nil {
		t.Errorf("rep %v, err %v; want nothing", rep, err)
	}
}

// pluginServer is the plugin's listener as the hook meets it: connect.json
// in a plugin folder, a token, the /project route.
func pluginServer(t *testing.T, h *Host) (dir string, token string) {
	t.Helper()
	token = "secret-token"
	mux := http.NewServeMux()
	mux.HandleFunc("POST /project", h.handleProject)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+token {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		mux.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)
	u, _ := url.Parse(srv.URL)
	dir = t.TempDir()
	raw, _ := json.Marshal(branchkit.ConnectInfo{Port: u.Port(), Token: token})
	if err := os.WriteFile(filepath.Join(dir, "connect.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	return dir, token
}

func TestTheHookSendsTheProjectToThePlugin(t *testing.T) {
	repo := gitRepo(t)
	f := newFakePlatform()
	h := newHost(f, englishLexicon())
	pluginDir, _ := pluginServer(t, h)
	t.Setenv("BRANCHKIT_PLUGIN_DIR", pluginDir)

	in, _ := json.Marshal(hookInput{Cwd: repo, SessionID: "s1", HookEventName: "UserPromptSubmit"})
	if err := claudeHook(context.Background(), bytes.NewReader(in)); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return f.has(filesCollection, "mission") })
	if err := h.cite("mission"); err != nil {
		t.Fatal(err)
	}
	if got := f.typedText(); !reflect.DeepEqual(got, []string{"@docs/MISSION.md "}) {
		t.Errorf("typed %q", got)
	}
}

func TestTheHookIsRefusedWithoutTheToken(t *testing.T) {
	h := newHost(newFakePlatform(), testLex)
	pluginDir, _ := pluginServer(t, h)
	raw, _ := os.ReadFile(filepath.Join(pluginDir, "connect.json"))
	var info branchkit.ConnectInfo
	_ = json.Unmarshal(raw, &info)
	info.Token = "wrong"
	err := sendReport(context.Background(), info, &projectReport{Cwd: "/x"})
	if err == nil || !strings.Contains(err.Error(), "403") {
		t.Errorf("err = %v, want a refusal", err)
	}
}

// The hook never blocks or pollutes the prompt: whatever goes wrong, it
// exits 0 and writes only to stderr.
func TestTheHookNeverFailsThePrompt(t *testing.T) {
	t.Setenv("BRANCHKIT_PLUGIN_DIR", t.TempDir()) // no connect.json: plugin not running
	repo := gitRepo(t)
	for _, input := range []string{
		"not json",
		`{"cwd":""}`,
		`{"cwd":"` + repo + `"}`,
	} {
		var stderr bytes.Buffer
		if code := runClaudeHook(strings.NewReader(input), &stderr); code != 0 {
			t.Errorf("%s: exit %d", input, code)
		}
		if stderr.Len() == 0 {
			t.Errorf("%s: the problem should be reported on stderr", input)
		}
	}
}

func TestFindPluginReportsAPluginThatIsNotRunning(t *testing.T) {
	t.Setenv("BRANCHKIT_PLUGIN_DIR", t.TempDir())
	if _, err := findPlugin(); err == nil || !strings.Contains(err.Error(), "not running") {
		t.Errorf("err = %v", err)
	}
}

// A file removed from disk but still in the index is not named, and does
// not rank first as changed.
func TestListProjectLeavesOutDeletedFiles(t *testing.T) {
	dir := gitRepo(t)
	if err := os.Remove(filepath.Join(dir, "docs", "DEV_LOOP.md")); err != nil {
		t.Fatal(err)
	}
	rep, err := listProject(context.Background(), dir)
	if err != nil || rep == nil {
		t.Fatalf("rep %v, err %v", rep, err)
	}
	for _, list := range [][]string{rep.Files, rep.Changed} {
		for _, p := range list {
			if p == "docs/DEV_LOOP.md" {
				t.Errorf("deleted file listed: files %q changed %q", rep.Files, rep.Changed)
			}
		}
	}
}

// Moving to a folder that is not a git project clears the previous
// project's names: their paths are relative to another folder.
func TestTheHookOutsideGitClearsTheOldProject(t *testing.T) {
	repo := gitRepo(t)
	f := newFakePlatform()
	h := newHost(f, englishLexicon())
	pluginDir, _ := pluginServer(t, h)
	t.Setenv("BRANCHKIT_PLUGIN_DIR", pluginDir)

	send := func(cwd string) {
		t.Helper()
		in, _ := json.Marshal(hookInput{Cwd: cwd, HookEventName: "UserPromptSubmit"})
		if err := claudeHook(context.Background(), bytes.NewReader(in)); err != nil {
			t.Fatal(err)
		}
	}
	send(repo)
	waitFor(t, func() bool { return f.has(filesCollection, "mission") })
	elsewhere := t.TempDir()
	send(elsewhere)
	waitFor(t, func() bool { return f.count(filesCollection) == 0 })
	if err := h.cite("mission"); err == nil {
		t.Error("the old project's name must not answer in a folder outside git")
	}
	if len(f.typedText()) != 0 {
		t.Errorf("typed %q", f.typedText())
	}
}

// A repository far past maxFiles still reports: the hook keeps the most
// likely files (changed first, then nearest the root), and the body fits.
func TestALargeRepositoryReportFitsTheLimit(t *testing.T) {
	var files []string
	for i := 0; i < 3*maxFiles; i++ {
		files = append(files, fmt.Sprintf("third_party/chromium/src/deep/path/component_%06d/file_%06d.cc", i, i))
	}
	files = append(files, "docs/MISSION.md", "bad path.md")
	changed := []string{"third_party/chromium/src/deep/path/component_000007/file_000007.cc"}
	kept, keptChanged := capReport(files, changed)
	if len(kept) != maxFiles {
		t.Fatalf("kept %d files, want %d", len(kept), maxFiles)
	}
	if kept[0] != changed[0] || kept[1] != "docs/MISSION.md" {
		t.Errorf("head = %q, want the changed file, then the shallowest", kept[:2])
	}
	if !reflect.DeepEqual(keptChanged, changed) {
		t.Errorf("changed = %q", keptChanged)
	}
	for _, p := range kept {
		if p == "bad path.md" {
			t.Error("an unmentionable path was sent")
		}
	}
	body, _ := json.Marshal(projectReport{Cwd: "/r", Files: kept, Changed: keptChanged})
	if len(body) > maxReportBytes {
		t.Errorf("body %d bytes exceeds the plugin's %d", len(body), maxReportBytes)
	}
}
