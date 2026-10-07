package main

import (
	"fmt"
	"reflect"
	"sort"
	"testing"
)

var testLex = fakeLexicon("docs", "mission", "dev", "loop", "matching", "service",
	"services", "sandbox", "mod", "main", "voice", "apps", "plugins", "names",
	"foo", "bar", "notes", "loop", "read", "me", "a", "b", "x", "lib", "util")

// namesOf is every name table gives one file, sorted.
func namesOf(table nameTable, path string) []string {
	var out []string
	for s, paths := range table {
		if len(paths) == 1 && paths[0] == path {
			out = append(out, s)
		}
	}
	sort.Strings(out)
	return out
}

func TestAUniqueFileIsNamedByItsNameAlone(t *testing.T) {
	table := nameFiles([]string{"docs/MISSION.md", "docs/DEV_LOOP.md", "src/foo.go"}, nil, testLex)
	if got := table["mission"]; !reflect.DeepEqual(got, []string{"docs/MISSION.md"}) {
		t.Errorf("mission = %q", got)
	}
	if got := table["dev loop"]; !reflect.DeepEqual(got, []string{"docs/DEV_LOOP.md"}) {
		t.Errorf("dev loop = %q", got)
	}
	// Rule 5: the extension is never needed when the name is unique, and
	// the folder is not either.
	for _, s := range []string{"mission markdown", "docs mission", "foo go"} {
		if table[s] != nil {
			t.Errorf("%q should not be a name: a shorter one is unique", s)
		}
	}
}

// Rule 7: files that share a name are named by the shortest end of their
// path that is theirs alone, as VS Code labels editor tabs.
func TestSharedNamesTakeTheShortestUniquePathEnd(t *testing.T) {
	files := []string{"apps/src/names.go", "voice/src/names.go", "voice/lib/names.go"}
	table := nameFiles(files, nil, testLex)
	if got := namesOf(table, "apps/src/names.go"); !reflect.DeepEqual(got, []string{"apps names"}) {
		t.Errorf("apps/src/names.go = %q", got)
	}
	if got := namesOf(table, "voice/lib/names.go"); !reflect.DeepEqual(got, []string{"lib names"}) {
		t.Errorf("voice/lib/names.go = %q", got)
	}
	// "s r c names" and "voice names" are both shared; the nearest-folders
	// end "voice s r c names" is not.
	if got := namesOf(table, "voice/src/names.go"); !reflect.DeepEqual(got, []string{"voice s r c names"}) {
		t.Errorf("voice/src/names.go = %q", got)
	}
}

// A path that is another's tail still names its own file: "a x" is a/x.go's
// whole path, even though b/a/x.go also ends in it.
func TestAWholePathBelongsToItsFile(t *testing.T) {
	table := nameFiles([]string{"a/x.go", "b/a/x.go"}, nil, testLex)
	if got := table["a x"]; !reflect.DeepEqual(got, []string{"a/x.go"}) {
		t.Errorf("a x = %q", got)
	}
	if got := table["b x"]; !reflect.DeepEqual(got, []string{"b/a/x.go"}) {
		t.Errorf("b x = %q", got)
	}
}

// Rule 5: the extension separates names that are otherwise equal.
func TestTheExtensionSeparatesEqualNames(t *testing.T) {
	table := nameFiles([]string{"lib/names.rs", "lib/names.go"}, nil, testLex)
	if got := table["lib names rust"]; !reflect.DeepEqual(got, []string{"lib/names.rs"}) {
		t.Errorf("lib names rust = %q", got)
	}
	if got := table["lib names go"]; !reflect.DeepEqual(got, []string{"lib/names.go"}) {
		t.Errorf("lib names go = %q", got)
	}
	// Said without it, it is a choice between them.
	if got := table["names"]; len(got) != 2 {
		t.Errorf("names = %q, want both files", got)
	}
}

// Rule 6: a generic name is said with its folder, even when it is unique.
func TestGenericNamesTakeTheirFolder(t *testing.T) {
	table := nameFiles([]string{"sandbox/mod.rs", "voice/main.go", "README.md"}, nil, testLex)
	if got := namesOf(table, "sandbox/mod.rs"); !reflect.DeepEqual(got, []string{"sandbox mod"}) {
		t.Errorf("sandbox/mod.rs = %q", got)
	}
	if table["mod"] != nil || table["main"] != nil {
		t.Errorf("a generic name alone must not be a name: %v %v", table["mod"], table["main"])
	}
	// At the project root there is no folder to take.
	if got := table["readme"]; !reflect.DeepEqual(got, []string{"README.md"}) {
		t.Errorf("readme = %q", got)
	}
}

// Ambiguity is never resolved silently: a short name several files share is
// published with all of them, the most likely first.
func TestASharedShortNameOffersEveryFileMostLikelyFirst(t *testing.T) {
	files := []string{"voice/stages/x/main.rs", "voice/src/main.go", "voice/stages/y/main.rs"}
	table := nameFiles(files, map[string]bool{"voice/stages/y/main.rs": true}, testLex)
	want := []string{"voice/stages/y/main.rs", "voice/src/main.go", "voice/stages/x/main.rs"}
	if got := table["voice main"]; !reflect.DeepEqual(got, want) {
		t.Errorf("voice main = %q, want %q (changed, then shallower, then by path)", got, want)
	}
}

// Two paths that read alike have no name of their own; their full names are
// a choice.
func TestPathsThatReadAlikeAreAChoice(t *testing.T) {
	table := nameFiles([]string{"notes/foo_bar.md", "notes/foo-bar.md"}, nil, testLex)
	if got := table["notes foo bar markdown"]; len(got) != 2 {
		t.Errorf("notes foo bar markdown = %q, want both", got)
	}
	if got := table["foo bar"]; len(got) != 2 {
		t.Errorf("foo bar = %q, want both", got)
	}
}

// A name shared by more files than there are badges is no help to say.
func TestANameSharedTooWidelyIsNotAName(t *testing.T) {
	var files []string
	for i := 0; i < maxChoices+1; i++ {
		files = append(files, fmt.Sprintf("d%c/notes.md", 'a'+i))
	}
	table := nameFiles(files, nil, testLex)
	if got := table["notes"]; got != nil {
		t.Errorf("notes = %q, want no name (%d files)", got, len(files))
	}
}

// Rule 8: one word of a file name names it when no other file has that word
// and it is not a word that could mean anything.
func TestAWordOnlyOneFileHasNamesIt(t *testing.T) {
	files := []string{"services/matching_service.rs", "services/sandbox_service.rs", "util/test_loop.go"}
	table := nameFiles(files, nil, testLex)
	if got := table["matching"]; !reflect.DeepEqual(got, []string{"services/matching_service.rs"}) {
		t.Errorf("matching = %q", got)
	}
	if got := table["service"]; got != nil {
		t.Errorf("service = %q: two files have it", got)
	}
	if got := table["test"]; got != nil {
		t.Errorf("test = %q: too general to name a file alone", got)
	}
	if got := table["loop"]; !reflect.DeepEqual(got, []string{"util/test_loop.go"}) {
		t.Errorf("loop = %q", got)
	}
}

func TestEveryNameIsSayable(t *testing.T) {
	files := []string{"docs/MISSION.md", "src/HTTPServer_v2.go", "naïve/café.txt", "x86_64/main.c", "日本/a.go", "_.go", "Makefile"}
	for s := range nameFiles(files, nil, englishLexicon()) {
		for _, r := range s {
			if (r < 'a' || r > 'z') && r != ' ' {
				t.Errorf("name %q has %q, which the engine cannot hear", s, r)
			}
		}
	}
}

func TestSplitName(t *testing.T) {
	cases := map[string][2]string{
		"DEV_LOOP.md":  {"DEV_LOOP", "md"},
		".gitignore":   {"gitignore", ""},
		".eslintrc.js": {"eslintrc", "js"},
		"foo.test.ts":  {"foo.test", "ts"},
		"Makefile":     {"Makefile", ""},
	}
	for in, want := range cases {
		if stem, ext := splitName(in); stem != want[0] || ext != want[1] {
			t.Errorf("splitName(%q) = %q, %q; want %q, %q", in, stem, ext, want[0], want[1])
		}
	}
}

// A file with nothing sayable in its name is named by its extension.
func TestANameWithNoWordsUsesItsExtension(t *testing.T) {
	table := nameFiles([]string{"_.go"}, nil, testLex)
	if got := table["go"]; !reflect.DeepEqual(got, []string{"_.go"}) {
		t.Errorf("go = %q", got)
	}
}

func TestRankFiles(t *testing.T) {
	got := rankFiles([]string{"b/c.go", "z.go", "a/b/c.go", "a.go"}, map[string]bool{"a/b/c.go": true})
	want := []string{"a/b/c.go", "a.go", "z.go", "b/c.go"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("rankFiles = %q, want %q", got, want)
	}
}
