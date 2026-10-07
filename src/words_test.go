package main

import (
	"reflect"
	"strings"
	"testing"
)

// fakeLexicon is a word list a test controls, so a rule's outcome does not
// depend on what the dictionary happens to contain.
func fakeLexicon(words ...string) lexicon {
	s := wordSet{}
	for _, w := range words {
		s[w] = struct{}{}
	}
	return s
}

func TestChunksSplitWhereAWordBreakIsHeard(t *testing.T) {
	cases := map[string][]string{
		"DEV_LOOP":         {"DEV", "LOOP"},
		"matching-service": {"matching", "service"},
		"foo.test":         {"foo", "test"},
		"matchingService":  {"matching", "Service"},
		"HTTPServer":       {"HTTP", "Server"},
		"PluginSDK":        {"Plugin", "SDK"},
		"v2":               {"v", "2"},
		"utf8":             {"utf", "8"},
		"x86_64":           {"x", "86", "64"},
		"IOError":          {"IO", "Error"},
		"a b":              {"a", "b"},
		"__init__":         {"init"},
		"":                 nil,
	}
	for in, want := range cases {
		if got := chunks(in); !reflect.DeepEqual(got, want) {
			t.Errorf("chunks(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestPieceForms(t *testing.T) {
	lex := fakeLexicon("dev", "loop", "mission", "read", "me", "key", "bind",
		"matching", "service", "server", "error", "plugin", "git", "ignore",
		"con", "fig", "change", "log", "naive", "cafe")
	cases := map[string][]string{
		// Rule 1: split on separators, case and digits.
		"DEV_LOOP":        {"dev loop"},
		"matchingService": {"matching service"},
		// Rule 2: an all-caps word the lexicon knows is the word; one it
		// does not, of up to four letters, is spelled.
		"MISSION":   {"mission"},
		"MCP":       {"m c p"},
		"PluginSDK": {"plugin s d k"},
		"IOError":   {"i o error"},
		"ABCDE":     {"abcde"},
		// A short chunk with no vowel is an abbreviation.
		"cfg": {"c f g"},
		"src": {"s r c"},
		// Three letters or fewer, unknown: spelled first, whole as well.
		"api": {"a p i", "api"},
		// Rule 3: run-together words also split, by the lexicon; the
		// run-together form stays first.
		"readme":    {"readme", "read me"},
		"README":    {"readme", "read me"},
		"keybind":   {"keybind", "key bind"},
		"gitignore": {"gitignore", "git ignore"},
		"config":    {"config", "con fig"},
		// An unknown word with no split is said as written.
		"sherpa": {"sherpa"},
		// Rule 4: numbers, naturally and digit by digit; v before a number
		// is also "version".
		"v2":   {"v two", "version two"},
		"07":   {"zero seven"},
		"utf8": {"u t f eight", "utf eight"},
		// Accents fold; letters the engine cannot hear are dropped.
		"naïve_café": {"naive cafe"},
		"日本":         nil,
		"___":        nil,
	}
	for in, want := range cases {
		if got := pieceForms(in, lex); !reflect.DeepEqual(got, want) {
			t.Errorf("pieceForms(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestPieceFormsAreCappedAndSayable(t *testing.T) {
	got := pieceForms("v2_x86_64_2026", fakeLexicon())
	if len(got) > maxForms {
		t.Fatalf("%d forms, cap is %d: %q", len(got), maxForms, got)
	}
	for _, f := range got {
		for _, r := range f {
			if (r < 'a' || r > 'z') && r != ' ' {
				t.Errorf("form %q has %q, which the engine cannot hear", f, r)
			}
		}
		if strings.Contains(f, "  ") || strings.TrimSpace(f) != f {
			t.Errorf("form %q is not single-spaced", f)
		}
	}
}

func TestSplitPacked(t *testing.T) {
	lex := fakeLexicon("read", "key", "bind", "change", "log", "git", "ignore", "ab", "abc", "keyb", "ind")
	cases := []struct {
		in   string
		want string
		ok   bool
	}{
		{"readme", "read me", true},   // "me" is a two-letter word
		{"keybind", "key bind", true}, // shortest part longest: not "keyb ind"
		{"changelog", "change log", true},
		{"gitignore", "git ignore", true},
		{"read", "", false},   // already one word: no split into two
		{"abxyz", "", false},  // no split covers it
		{"abread", "", false}, // "ab" is a dictionary entry but not a two-letter word
	}
	for _, c := range cases {
		got, ok := splitPacked(c.in, lex)
		if got != c.want || ok != c.ok {
			t.Errorf("splitPacked(%q) = %q, %v; want %q, %v", c.in, got, ok, c.want, c.ok)
		}
	}
}

func TestNumberForms(t *testing.T) {
	cases := map[string][]string{
		"0":      {"zero"},
		"7":      {"seven"},
		"12":     {"twelve", "one two"},
		"40":     {"forty", "four zero"},
		"101":    {"one hundred one", "one zero one"},
		"2026":   {"two thousand twenty six", "twenty twenty six", "two zero two six"},
		"2005":   {"two thousand five", "twenty oh five", "two zero zero five"},
		"2000":   {"two thousand", "two zero zero zero"},
		"007":    {"zero zero seven"},
		"123456": {"one hundred twenty three thousand four hundred fifty six", "one two three four five six"},
	}
	for in, want := range cases {
		if got := numberForms(in); !reflect.DeepEqual(got, want) {
			t.Errorf("numberForms(%q) = %q, want %q", in, got, want)
		}
	}
	// Past a million, digits only.
	if got := numberForms("1234567"); len(got) != 1 || got[0] != "one two three four five six seven" {
		t.Errorf("numberForms(1234567) = %q", got)
	}
}

func TestExtensionForms(t *testing.T) {
	lex := fakeLexicon("go", "sum", "json")
	cases := map[string][]string{
		"rs":   {"rust", "r s"},
		"md":   {"markdown", "m d"},
		"MD":   {"markdown", "m d"},
		"go":   {"go"},
		"json": {"json"},
		"html": {"h t m l"},
		"yml":  {"yaml", "y m l", "yml"},
	}
	for in, want := range cases {
		if got := extensionForms(in, lex); !reflect.DeepEqual(got, want) {
			t.Errorf("extensionForms(%q) = %q, want %q", in, got, want)
		}
	}
}

// The embedded word list is the real one: it decodes, it knows plain words,
// and it has dropped the abbreviations CMUdict pronounces letter by letter.
func TestEnglishLexicon(t *testing.T) {
	lex := englishLexicon()
	for _, w := range []string{"mission", "read", "loop", "service", "ignore"} {
		if !lex.word(w) {
			t.Errorf("%q should be a word", w)
		}
	}
	for _, w := range []string{"api", "md", "html", "readme", "m"} {
		if lex.word(w) {
			t.Errorf("%q should not be a word (spelled out, run together, or a letter)", w)
		}
	}
	if got := pieceForms("DEV_LOOP", lex); !reflect.DeepEqual(got, []string{"dev loop"}) {
		t.Errorf("DEV_LOOP = %q", got)
	}
	if got := pieceForms("README", lex); !reflect.DeepEqual(got, []string{"readme", "read me"}) {
		t.Errorf("README = %q", got)
	}
}
