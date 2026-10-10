package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

const testCwd = "/work/project"

// testDeck is the alphabet hostWith publishes, in the order the badges take
// it (sorted).
var testDeck = []string{"aim", "bus", "car", "day", "easy", "fame", "gold", "hat", "ice", "jam", "kit", "lime"}

func seedAlphabet(t *testing.T, f *fakePlatform) {
	t.Helper()
	for i, w := range testDeck {
		letter := string(rune('a' + i))
		if err := f.Put(alphabetCollection, letter, map[string]string{"codeword": w, "letter": letter}); err != nil {
			t.Fatal(err)
		}
	}
}

// hostWith returns a host whose published project has exactly these names.
func hostWith(t *testing.T, names nameTable) (*Host, *fakePlatform) {
	t.Helper()
	f := newFakePlatform()
	seedAlphabet(t, f)
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
	if len(f.states) != 0 {
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

// A name something else wrote to the collection is in the collection, not
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

func TestCitingAnAmbiguousNameOffersBadgedChoices(t *testing.T) {
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
		raw := f.collections[choicesCollection][testDeck[i]]
		var rec map[string]string
		if err := json.Unmarshal(raw, &rec); err != nil || rec["path"] != p || rec["codeword"] != testDeck[i] {
			t.Errorf("badge %q = %s, want %s", testDeck[i], raw, p)
		}
	}
	if !f.has(choosingTag, "singleton") {
		t.Error("the choosing gate should be set")
	}
	if len(f.states) != 1 {
		t.Fatalf("states %d", len(f.states))
	}
	doc := f.states[0]
	if doc.Channel != hudChannel || doc.Kind != "choices" || len(doc.Sections) != 1 || len(doc.Sections[0].Items) != 2 {
		t.Fatalf("doc = %+v", doc)
	}
	item := doc.Sections[0].Items[1]
	// The word to say is the title, the file beneath it.
	if item.Phrase != "bus" || item.Title != "bus" || item.Subtitle == nil || *item.Subtitle != "voice/stages/a/main.rs" {
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
	if n := f.count(choicesCollection); n != maxChoices {
		t.Errorf("%d choices, want %d", n, maxChoices)
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
	if f.cleared != 1 {
		t.Errorf("cleared %d, want the card cleared", f.cleared)
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
	if len(f.states) != 1 {
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
	if len(f.states) != 2 {
		t.Errorf("shown %d times, want the choice offered again", len(f.states))
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
	if len(f.typedText()) != 0 || len(f.states) != 1 {
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
	if len(f.typedText()) != 0 || f.has(choosingTag, "singleton") || f.cleared != 1 {
		t.Errorf("typed %q, gate %v, hidden %d", f.typedText(), f.has(choosingTag, "singleton"), f.cleared)
	}
	if f.count(learnedCollection) != 0 {
		t.Error("cancelling must not teach anything")
	}
	// Closing again (the hold ending after a cancel) does nothing more.
	h.closeChoices()
	if f.cleared != 1 {
		t.Errorf("hidden %d, want 1", f.cleared)
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

// The hold can end while a choice is still being written: the name decodes
// at the release, so the action and the hold's end arrive together. The end
// must still win: no gate left behind to swallow the next hold, no window.
func TestAHoldEndingMidOfferLeavesNothingOpen(t *testing.T) {
	paths := []string{"voice/src/main.go", "voice/stages/a/main.rs"}
	h, f := hostWith(t, nameTable{"voice main": paths})
	f.onPut = func(name string) {
		if name == choosingTag {
			f.onPut = nil
			h.closeChoices() // the session boundary, between the badges and the gate
		}
	}
	if err := h.cite("voice main"); err != nil {
		t.Fatal(err)
	}
	if f.has(choosingTag, "singleton") || f.count(choicesCollection) != 0 {
		t.Error("the gate or the badges outlived the hold")
	}
	if f.cleared == 0 {
		t.Error("the window outlived the hold")
	}
	h.mu.Lock()
	open := h.choosing != nil
	h.mu.Unlock()
	if open {
		t.Error("no choice should be open")
	}
}

// Without a published alphabet the choices are still sayable: numbered.
func TestWithoutAnAlphabetChoicesAreNumbered(t *testing.T) {
	h, f := hostWith(t, nameTable{"notes": {"a/notes.md", "b/notes.md"}})
	f.mu.Lock()
	delete(f.collections, alphabetCollection)
	f.mu.Unlock()
	if err := h.cite("notes"); err != nil {
		t.Fatal(err)
	}
	if !f.has(choicesCollection, "one") || !f.has(choicesCollection, "two") {
		t.Errorf("choices = %v", f.collections[choicesCollection])
	}
}

// A name whose other files are all tests, fixtures or vendored code types
// the one that is not, without asking.
func TestAClearWinnerIsTypedWithoutAsking(t *testing.T) {
	h, f := hostWith(t, nameTable{"mission": {
		"docs/MISSION.md",
		"branchkit-extension/test-fixtures/lifecycle/mission.html",
	}})
	if err := h.cite("mission"); err != nil {
		t.Fatal(err)
	}
	if got := f.typedText(); !reflect.DeepEqual(got, []string{"@docs/MISSION.md "}) {
		t.Errorf("typed %q", got)
	}
	if len(f.states) != 0 || f.count(learnedCollection) != 0 {
		t.Error("a clear winner neither asks nor teaches")
	}
}

// Two files that could both be meant are a real choice, whatever the rank.
func TestTwoRealCandidatesStillAsk(t *testing.T) {
	h, f := hostWith(t, nameTable{"notes": {"notes.md", "docs/notes.md", "vendor/x/notes.md"}})
	if err := h.cite("notes"); err != nil {
		t.Fatal(err)
	}
	if len(f.typedText()) != 0 || len(f.states) != 1 {
		t.Errorf("typed %q, shown %d", f.typedText(), len(f.states))
	}
}

func TestIncidental(t *testing.T) {
	for p, want := range map[string]bool{
		"docs/MISSION.md":                   false,
		"src/testing/helper.go":             false,
		"test/main_test.go":                 true,
		"a/b/test-fixtures/c/mission.html":  true,
		"web/node_modules/react/index.js":   true,
		"third_party/mime_guess/src/lib.rs": true,
		"Tests/AppTests/Fixtures/x.json":    true,
		"README.md":                         false,
	} {
		if got := incidental(p); got != want {
			t.Errorf("incidental(%q) = %v, want %v", p, got, want)
		}
	}
}

// "mention" alone lists the changed files, then the ones mentioned lately,
// each once, badged in alphabet order.
func TestBrowseListsChangedThenRecent(t *testing.T) {
	h, f := hostWith(t, nameTable{"mission": {"docs/MISSION.md"}, "plan": {"docs/PLAN.md"}})
	h.mu.Lock()
	h.project.changed = []string{"src/a.go", "src/b.go"}
	h.mu.Unlock()
	if err := h.cite("plan"); err != nil { // typed, so recent
		t.Fatal(err)
	}
	if err := h.insert("src/a.go"); err != nil { // changed AND recent: listed once
		t.Fatal(err)
	}
	if err := h.browse(); err != nil {
		t.Fatal(err)
	}
	doc := f.states[len(f.states)-1]
	var got []string
	for _, it := range doc.Sections[0].Items {
		got = append(got, it.Title+"="+*it.Subtitle)
	}
	want := []string{"aim=src/a.go", "bus=src/b.go", "car=docs/PLAN.md"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("list = %q, want %q", got, want)
	}
	if !f.has(browsingTag, "singleton") || f.has(choosingTag, "singleton") || !f.has(choicesCollection, "car") {
		t.Error("the list should add its badges beside everything else, not open the exclusive mode")
	}
	// A pick from the list answers no name, so it teaches none.
	if err := h.insert("src/b.go"); err != nil {
		t.Fatal(err)
	}
	if f.count(learnedCollection) != 0 {
		t.Error("a pick from the list must not be learned as a name")
	}
}

func TestBrowseIsCappedAtOneWordEach(t *testing.T) {
	var changed []string
	for i := 0; i < 40; i++ {
		changed = append(changed, fmt.Sprintf("f%02d.go", i))
	}
	h, f := hostWith(t, nameTable{})
	h.mu.Lock()
	h.project.changed = changed
	h.mu.Unlock()
	if err := h.browse(); err != nil {
		t.Fatal(err)
	}
	// The test alphabet has 12 words: the list stops where the words do.
	if n := f.count(choicesCollection); n != len(testDeck) {
		t.Errorf("%d listed, want %d", n, len(testDeck))
	}
}

// Nothing changed or mentioned yet: the window says so, and names still work.
func TestAnEmptyBrowseSaysSo(t *testing.T) {
	h, f := hostWith(t, nameTable{})
	if err := h.browse(); err != nil {
		t.Fatal(err)
	}
	doc := f.states[len(f.states)-1]
	if len(doc.Sections) != 0 || !strings.Contains(doc.Phrase, "a file's name") {
		t.Errorf("doc = %+v", doc)
	}
}

// The recent list belongs to its project: another project starts it afresh.
func TestRecentFollowsTheProject(t *testing.T) {
	h, _ := hostWith(t, nameTable{"plan": {"docs/PLAN.md"}})
	h.noteRecent("a.go")
	h.noteRecent("b.go")
	h.noteRecent("a.go")
	h.mu.Lock()
	got := append([]string(nil), h.recentIn(testCwd)...)
	h.project.cwd = "/other"
	h.mu.Unlock()
	if want := []string{"a.go", "b.go"}; !reflect.DeepEqual(got, want) {
		t.Errorf("recent = %q, want %q", got, want)
	}
	h.noteRecent("c.go")
	h.mu.Lock()
	defer h.mu.Unlock()
	if got := h.recentIn("/other"); !reflect.DeepEqual(got, []string{"c.go"}) {
		t.Errorf("recent in another project = %q", got)
	}
	if h.recentIn(testCwd) != nil {
		t.Error("the old project's recent files must not show in the new one")
	}
}

// The list "mention" alone opens outlives the hold, so it can be picked
// with the pointer, the keys or a switch; "which file?" ends with the hold.
func TestTheListOutlivesTheHoldAndWhichFileDoesNot(t *testing.T) {
	h, f := hostWith(t, nameTable{"notes": {"a/notes.md", "b/notes.md"}})
	h.mu.Lock()
	h.project.changed = []string{"src/a.go"}
	h.mu.Unlock()
	if err := h.browse(); err != nil {
		t.Fatal(err)
	}
	h.holdEnded()
	if f.cleared != 0 || !f.has(browsingTag, "singleton") {
		t.Fatalf("the list or its badges closed at the hold's end (%d clears)", f.cleared)
	}
	if err := h.insert("src/a.go"); err != nil { // picked later, by pointer
		t.Fatal(err)
	}
	if got := f.typedText(); len(got) != 1 || f.cleared != 1 {
		t.Errorf("typed %q, clears %d: a later pick types and closes", got, f.cleared)
	}

	if err := h.cite("notes"); err != nil {
		t.Fatal(err)
	}
	h.holdEnded()
	if f.cleared != 2 || f.has(choosingTag, "singleton") {
		t.Errorf("clears %d: which file? should close with the hold", f.cleared)
	}
}

// The list "mention" opens is there after a restart: the changed and recent
// files are kept, and a new run reads them back before any report arrives.
func TestTheMentionListSurvivesARestart(t *testing.T) {
	h, f := hostWith(t, nameTable{"plan": {"docs/PLAN.md"}})
	h.mu.Lock()
	h.project.changed = []string{"src/a.go"}
	h.mu.Unlock()
	if err := h.insert("docs/PLAN.md"); err != nil { // recent, and kept
		t.Fatal(err)
	}
	if !f.has(latelyCollection, testCwd) {
		t.Fatal("the list was not kept")
	}

	again := newHost(f, testLex) // a restart: same platform, nothing in memory
	if err := again.loadProject(); err != nil {
		t.Fatal(err)
	}
	if err := again.loadLately(); err != nil {
		t.Fatal(err)
	}
	if err := again.browse(); err != nil {
		t.Fatal(err)
	}
	doc := f.states[len(f.states)-1]
	var got []string
	for _, it := range doc.Sections[0].Items {
		got = append(got, *it.Subtitle)
	}
	if want := []string{"src/a.go", "docs/PLAN.md"}; !reflect.DeepEqual(got, want) {
		t.Errorf("after a restart the list is %q, want %q", got, want)
	}
}
