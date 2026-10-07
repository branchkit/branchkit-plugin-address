package main

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
)

const testCwd = "/work/project"

// hostWith returns a host whose published project has exactly these names.
func hostWith(t *testing.T, names nameTable) (*Host, *fakePlatform) {
	t.Helper()
	f := newFakePlatform()
	h := newHost(f, testLex)
	entries, err := fileEntries(testCwd, names)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Replace(filesCollection, entries, scopeForTest()); err != nil {
		t.Fatal(err)
	}
	if err := h.loadProject(); err != nil {
		t.Fatal(err)
	}
	return h, f
}

func TestCitingAUniqueNameTypesItsMention(t *testing.T) {
	h, f := hostWith(t, nameTable{"mission": {"docs/MISSION.md"}})
	if err := h.cite("mission"); err != nil {
		t.Fatal(err)
	}
	if got := f.typedText(); !reflect.DeepEqual(got, []string{"@docs/MISSION.md "}) {
		t.Errorf("typed %q", got)
	}
	if f.shown != 0 {
		t.Error("a unique name must not open the choices")
	}
}

func TestCitingAnUnknownNameSaysSoAndTypesNothing(t *testing.T) {
	h, f := hostWith(t, nameTable{"mission": {"docs/MISSION.md"}})
	err := h.cite("roadmap")
	if err == nil || !strings.Contains(err.Error(), "roadmap") {
		t.Fatalf("err = %v, want one naming the name", err)
	}
	if len(f.typedText()) != 0 {
		t.Error("nothing should be typed")
	}
}

// A name the person added in the Collections page is in the collection, not
// in what was published; cite reads it back.
func TestCitingANameThePersonAddedReadsItBack(t *testing.T) {
	h, f := hostWith(t, nameTable{"mission": {"docs/MISSION.md"}})
	if err := f.Put(filesCollection, "the plan", fileRecord{Spoken: "the plan", Paths: []string{"docs/PLAN.md"}}); err != nil {
		t.Fatal(err)
	}
	if err := h.cite("the plan"); err != nil {
		t.Fatal(err)
	}
	if got := f.typedText(); !reflect.DeepEqual(got, []string{"@docs/PLAN.md "}) {
		t.Errorf("typed %q", got)
	}
}

func TestCitingAnAmbiguousNameOffersNumberedChoices(t *testing.T) {
	paths := []string{"voice/src/main.go", "voice/stages/a/main.rs"}
	h, f := hostWith(t, nameTable{"voice main": paths})
	if err := h.cite("voice main"); err != nil {
		t.Fatal(err)
	}
	if len(f.typedText()) != 0 {
		t.Fatal("an ambiguous name must not type anything until the person picks")
	}
	// The badge words are what the choosing mode hears; each picks its file.
	for i, p := range paths {
		raw := f.collections[choicesCollection][badges[i]]
		var rec map[string]string
		if err := json.Unmarshal(raw, &rec); err != nil || rec["path"] != p || rec["codeword"] != badges[i] {
			t.Errorf("badge %q = %s, want %s", badges[i], raw, p)
		}
	}
	if !f.has(choosingTag, "singleton") {
		t.Error("the choosing gate should be set")
	}
	if f.shown != 1 || len(f.states) != 1 {
		t.Fatalf("shown %d, states %d", f.shown, len(f.states))
	}
	doc := f.states[0]
	if doc.Channel != hudChannel || doc.Kind != "choices" || len(doc.Sections) != 1 || len(doc.Sections[0].Items) != 2 {
		t.Fatalf("doc = %+v", doc)
	}
	item := doc.Sections[0].Items[1]
	if item.Phrase != "two" || !strings.Contains(item.Title, "voice/stages/a/main.rs") {
		t.Errorf("item = %+v", item)
	}
	// Any renderer can confirm an item without the voice path.
	if item.Action == nil || item.Action.Dispatch == nil || *item.Action.Dispatch != "address.insert" ||
		!strings.Contains(string(item.Action.Params), "voice/stages/a/main.rs") {
		t.Errorf("item action = %+v", item.Action)
	}
	if !strings.Contains(doc.Phrase, "voice main") {
		t.Errorf("phrase %q should say what was asked", doc.Phrase)
	}
}

func TestChoicesAreCappedAtNine(t *testing.T) {
	var paths []string
	for i := 0; i < 12; i++ {
		paths = append(paths, "d"+string(rune('a'+i))+"/notes.md")
	}
	h, f := hostWith(t, nameTable{"notes": paths})
	if err := h.cite("notes"); err != nil {
		t.Fatal(err)
	}
	if n := f.count(choicesCollection); n != len(badges) {
		t.Errorf("%d choices, want %d", n, len(badges))
	}
}

// Picking types the file, closes everything, and teaches the name: next time
// in this project it types that file at once.
func TestPickingTypesTheFileAndIsRemembered(t *testing.T) {
	paths := []string{"voice/src/main.go", "voice/stages/a/main.rs"}
	h, f := hostWith(t, nameTable{"voice main": paths})
	if err := h.cite("voice main"); err != nil {
		t.Fatal(err)
	}
	if err := h.insert("voice/stages/a/main.rs"); err != nil {
		t.Fatal(err)
	}
	if got := f.typedText(); !reflect.DeepEqual(got, []string{"@voice/stages/a/main.rs "}) {
		t.Errorf("typed %q", got)
	}
	if f.has(choosingTag, "singleton") || f.count(choicesCollection) != 0 {
		t.Error("picking must close the choosing gate and the badges")
	}
	if f.cleared != 1 || f.hidden != 1 {
		t.Errorf("cleared %d hidden %d, want the window closed", f.cleared, f.hidden)
	}
	if !f.has(learnedCollection, learnedID(testCwd, "voice main")) {
		t.Fatal("the answer should be recorded where the person can see it")
	}

	if err := h.cite("voice main"); err != nil {
		t.Fatal(err)
	}
	if got := f.typedText(); len(got) != 2 || got[1] != "@voice/stages/a/main.rs " {
		t.Errorf("second cite typed %q, want the remembered file", got)
	}
	if f.shown != 1 {
		t.Error("a remembered answer must not ask again")
	}
}

// Deleting the record of an answer means being asked again.
func TestDeletingAnAnswerAsksAgain(t *testing.T) {
	paths := []string{"a/notes.md", "b/notes.md"}
	h, f := hostWith(t, nameTable{"notes": paths})
	_ = h.cite("notes")
	_ = h.insert("b/notes.md")
	if _, err := f.Delete(learnedCollection, learnedID(testCwd, "notes")); err != nil {
		t.Fatal(err)
	}
	if err := h.cite("notes"); err != nil {
		t.Fatal(err)
	}
	if f.shown != 2 {
		t.Errorf("shown %d times, want the choice offered again", f.shown)
	}
}

// An answer is for one project: the same name elsewhere still asks.
func TestAnAnswerIsPerProject(t *testing.T) {
	paths := []string{"a/notes.md", "b/notes.md"}
	h, f := hostWith(t, nameTable{"notes": paths})
	if err := f.Put(learnedCollection, learnedID("/other", "notes"), learnedRecord{Cwd: "/other", Spoken: "notes", Path: "b/notes.md"}); err != nil {
		t.Fatal(err)
	}
	if err := h.cite("notes"); err != nil {
		t.Fatal(err)
	}
	if len(f.typedText()) != 0 || f.shown != 1 {
		t.Error("another project's answer must not choose here")
	}
}

// A file typed by another route (a keybind, a click) while a choice is open
// is not an answer to it unless it is one of the choices.
func TestInsertingAnUnrelatedFileTeachesNothing(t *testing.T) {
	h, f := hostWith(t, nameTable{"notes": {"a/notes.md", "b/notes.md"}})
	_ = h.cite("notes")
	if err := h.insert("docs/MISSION.md"); err != nil {
		t.Fatal(err)
	}
	if f.count(learnedCollection) != 0 {
		t.Error("an unrelated insert must not be learned")
	}
}

func TestDismissClosesWithoutTyping(t *testing.T) {
	h, f := hostWith(t, nameTable{"notes": {"a/notes.md", "b/notes.md"}})
	_ = h.cite("notes")
	h.closeChoices()
	if len(f.typedText()) != 0 || f.has(choosingTag, "singleton") || f.hidden != 1 {
		t.Errorf("typed %q, gate %v, hidden %d", f.typedText(), f.has(choosingTag, "singleton"), f.hidden)
	}
	if f.count(learnedCollection) != 0 {
		t.Error("cancelling must not teach anything")
	}
	// Closing again (the hold ending after a cancel) does nothing more.
	h.closeChoices()
	if f.hidden != 1 {
		t.Errorf("hidden %d, want 1", f.hidden)
	}
}

func TestInsertRefusesAPathOutsideTheProject(t *testing.T) {
	h, f := hostWith(t, nameTable{})
	for _, p := range []string{"", "/etc/passwd", "../secret", "a/../../b", "my file.md"} {
		if err := h.insert(p); err == nil {
			t.Errorf("insert(%q) should be refused", p)
		}
	}
	if len(f.typedText()) != 0 {
		t.Errorf("typed %q", f.typedText())
	}
}

func TestATypingFailureIsReported(t *testing.T) {
	h, f := hostWith(t, nameTable{"mission": {"docs/MISSION.md"}})
	f.typeErr = errors.New("no input privilege")
	if err := h.cite("mission"); err == nil || !strings.Contains(err.Error(), "no input privilege") {
		t.Errorf("err = %v", err)
	}
}
