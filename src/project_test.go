package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"github.com/branchkit/plugin-sdk-go"
)

func scopeForTest() branchkit.ReplaceScope { return branchkit.ScopeCollection() }

func TestPublishMakesTheNamesTheCollection(t *testing.T) {
	f := newFakePlatform()
	h := newHost(f, testLex)
	rep := projectReport{Cwd: testCwd, Files: []string{"docs/MISSION.md", "docs/DEV_LOOP.md"}}
	if err := h.publish(rep); err != nil {
		t.Fatal(err)
	}
	var rec fileRecord
	if err := json.Unmarshal(f.collections[filesCollection]["mission"], &rec); err != nil {
		t.Fatal(err)
	}
	want := fileRecord{Spoken: "mission", Paths: []string{"docs/MISSION.md"}, Cwd: testCwd}
	if !reflect.DeepEqual(rec, want) {
		t.Errorf("record = %+v, want %+v", rec, want)
	}
	if err := h.cite("dev loop"); err != nil {
		t.Fatal(err)
	}
	if got := f.typedText(); !reflect.DeepEqual(got, []string{"@docs/DEV_LOOP.md "}) {
		t.Errorf("typed %q", got)
	}
}

// The newest report replaces the project: names of the old one are gone.
func TestANewProjectReplacesTheOld(t *testing.T) {
	f := newFakePlatform()
	h := newHost(f, testLex)
	_ = h.publish(projectReport{Cwd: "/a", Files: []string{"docs/MISSION.md"}})
	_ = h.publish(projectReport{Cwd: "/b", Files: []string{"notes.md"}})
	if f.has(filesCollection, "mission") || !f.has(filesCollection, "notes") {
		t.Errorf("collection = %v", f.collections[filesCollection])
	}
	if err := h.cite("mission"); err == nil {
		t.Error("a name from the previous project must not resolve")
	}
}

// Most prompts change nothing; an identical report is not republished.
func TestAnIdenticalReportIsSkipped(t *testing.T) {
	f := newFakePlatform()
	h := newHost(f, testLex)
	rep := projectReport{Cwd: testCwd, Files: []string{"a.md", "b.md"}, Changed: []string{"a.md"}}
	_ = h.publish(rep)
	_ = h.publish(rep)
	if f.replaces != 1 {
		t.Errorf("%d replaces, want 1", f.replaces)
	}
	rep.Changed = nil // what changed reorders the choices, so it counts
	_ = h.publish(rep)
	if f.replaces != 2 {
		t.Errorf("%d replaces, want 2", f.replaces)
	}
}

// Paths a mention cannot carry, or that point outside the folder, are never
// named.
func TestUnmentionablePathsAreDropped(t *testing.T) {
	f := newFakePlatform()
	h := newHost(f, testLex)
	_ = h.publish(projectReport{Cwd: testCwd, Files: []string{"my notes.md", "../escape.md", "/abs.md", "./x/../y.md", "ok.md", "ok.md"}})
	for id, raw := range f.collections[filesCollection] {
		var rec fileRecord
		_ = json.Unmarshal(raw, &rec)
		for _, p := range rec.Paths {
			if p != "ok.md" {
				t.Errorf("%q names %q", id, p)
			}
		}
	}
	if !f.has(filesCollection, "ok") {
		t.Error("ok.md should be named")
	}
}

func TestMentionable(t *testing.T) {
	for p, want := range map[string]bool{
		"docs/MISSION.md": true, ".gitignore": true, "a/b/c.go": true,
		"": false, ".": false, "..": false, "../a": false, "a/../b": false,
		"/a": false, "a b": false, "a\tb": false, "a//b": false, "./a": false,
	} {
		if got := mentionable(p); got != want {
			t.Errorf("mentionable(%q) = %v, want %v", p, got, want)
		}
	}
}

// Past maxFiles, the most likely files are the ones named.
func TestALargeProjectKeepsTheMostLikelyFiles(t *testing.T) {
	f := newFakePlatform()
	h := newHost(f, testLex)
	var files []string
	for i := 0; i < maxFiles+50; i++ {
		files = append(files, fmt.Sprintf("deep/dir/f%d.txt", i))
	}
	files = append(files, "deep/dir/sub/changed_notes.md")
	_ = h.publish(projectReport{Cwd: testCwd, Files: files, Changed: []string{"deep/dir/sub/changed_notes.md"}})
	h.mu.Lock()
	names := h.project.names
	h.mu.Unlock()
	found := false
	for _, paths := range names {
		for _, p := range paths {
			if p == "deep/dir/sub/changed_notes.md" {
				found = true
			}
		}
	}
	if !found {
		t.Error("a changed file must survive the cap")
	}
}

func TestHandleProjectAcceptsAndPublishes(t *testing.T) {
	f := newFakePlatform()
	h := newHost(f, testLex)
	body, _ := json.Marshal(projectReport{Cwd: testCwd, Files: []string{"docs/MISSION.md"}})
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
}

func TestHandleProjectRefusesABadReport(t *testing.T) {
	h := newHost(newFakePlatform(), testLex)
	for _, body := range []string{`not json`, `{"cwd":"relative/dir","files":[]}`, `{"files":["a"]}`} {
		rec := httptest.NewRecorder()
		h.handleProject(rec, httptest.NewRequest(http.MethodPost, "/project", bytes.NewReader([]byte(body))))
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status %d, want 400", body, rec.Code)
		}
	}
}

// Reports arriving while one publishes collapse to the newest.
func TestQueuedReportsCollapseToTheNewest(t *testing.T) {
	f := newFakePlatform()
	h := newHost(f, testLex)
	for i := 0; i < 20; i++ {
		h.submit(projectReport{Cwd: fmt.Sprintf("/p%d", i), Files: []string{"notes.md"}})
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		h.queueMu.Lock()
		idle := !h.publishing
		h.queueMu.Unlock()
		h.mu.Lock()
		cwd := h.project.cwd
		h.mu.Unlock()
		if idle && cwd == "/p19" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("final project %q, idle %v; want /p19", cwd, idle)
		}
		time.Sleep(5 * time.Millisecond)
	}
	if f.replaces > 20 {
		t.Errorf("%d replaces", f.replaces)
	}
}

// A restarted plugin answers from what it last published.
func TestLoadProjectRestoresThePublishedNames(t *testing.T) {
	f := newFakePlatform()
	_ = newHost(f, testLex).publish(projectReport{Cwd: testCwd, Files: []string{"docs/MISSION.md"}})
	h := newHost(f, testLex)
	if err := h.loadProject(); err != nil {
		t.Fatal(err)
	}
	if err := h.cite("mission"); err != nil {
		t.Fatal(err)
	}
	if h.project.cwd != testCwd {
		t.Errorf("cwd %q", h.project.cwd)
	}
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("timed out")
		}
		time.Sleep(5 * time.Millisecond)
	}
}
