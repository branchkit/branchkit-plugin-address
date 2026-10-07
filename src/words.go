package main

import (
	"strconv"
	"strings"
	"unicode"
)

// How one piece of a path (a folder name, a file name without its extension,
// an extension) becomes the words a person says for it. Everything here
// produces lowercase a-z words separated by single spaces: the speech engine
// can only hear words it can spell in lowercase letters, so a digit, a
// capital or an accent that reached it would make the name unsayable.

// maxForms caps how many ways one piece may be said. Alternatives multiply
// across the pieces of a path, and each one is a name the engine must hold.
const maxForms = 4

// chunks splits a piece of a name where a person hears a word break:
// punctuation (`_ - .` and anything else that is not a letter or digit), a
// lowercase letter followed by a capital (matchingService), the last capital
// of a run followed by lowercase (HTTPServer), and letters meeting digits
// (v2, utf8).
func chunks(s string) []string {
	var out []string
	for _, field := range strings.FieldsFunc(s, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	}) {
		rs := []rune(field)
		start := 0
		for i := 1; i < len(rs); i++ {
			prev, cur := rs[i-1], rs[i]
			boundary := unicode.IsDigit(prev) != unicode.IsDigit(cur) ||
				(unicode.IsLower(prev) && unicode.IsUpper(cur)) ||
				(unicode.IsUpper(prev) && unicode.IsUpper(cur) && i+1 < len(rs) && unicode.IsLower(rs[i+1]))
			if boundary {
				out = append(out, string(rs[start:i]))
				start = i
			}
		}
		out = append(out, string(rs[start:]))
	}
	return out
}

// pieceForms is every way to say one piece of a name, the most natural
// first. Empty when nothing in it can be said (a name of only symbols).
func pieceForms(s string, lex lexicon) []string {
	cs := chunks(s)
	forms := []string{""}
	for i, c := range cs {
		nextIsNumber := i+1 < len(cs) && isDigits(cs[i+1])
		alts := chunkForms(c, nextIsNumber, lex)
		if len(alts) == 0 {
			continue
		}
		var next []string
		for _, f := range forms {
			for _, a := range alts {
				next = append(next, strings.TrimSpace(f+" "+a))
			}
		}
		forms = dedupe(next)
		if len(forms) > maxForms {
			forms = forms[:maxForms]
		}
	}
	if len(forms) == 1 && forms[0] == "" {
		return nil
	}
	return forms
}

// chunkForms is how one chunk is said: the most natural form first.
//
//   - A number reads naturally ("twelve") and digit by digit ("one two").
//   - A word the lexicon knows is itself, whatever its case ("MISSION" →
//     "mission").
//   - An abbreviation is spelled: a capitalised chunk of up to four letters
//     that is not a word ("MCP" → "m c p"), or any short chunk with no vowel
//     ("cfg", "src"). Any other chunk of three letters or fewer that is not
//     a word is most likely one too ("api", "cli"), but may be said whole
//     ("env"), so it is both.
//   - Words run together are also said split where the lexicon finds words
//     ("readme" → "read me"); the run-together form stays, since the engine
//     can spell it, and stays first, since a wrong split ("config" → "con
//     fig") must not become what the file is known by.
//   - "v" before a number is also "version" ("v2" → "v two", "version two").
//   - Anything else is said as written ("sherpa", "config").
func chunkForms(c string, nextIsNumber bool, lex lexicon) []string {
	if isDigits(c) {
		return numberForms(c)
	}
	w := foldLetters(c)
	if w == "" {
		return nil
	}
	if len(w) == 1 {
		if w == "v" && nextIsNumber {
			return []string{"v", "version"}
		}
		return []string{w}
	}
	if lex.word(w) {
		return []string{w}
	}
	if (isUpper(c) && len(w) <= 4) || (!hasVowel(w) && len(w) <= 5) {
		return []string{spell(w)}
	}
	if len(w) <= 3 {
		return []string{spell(w), w}
	}
	if split, ok := splitPacked(w, lex); ok {
		return []string{w, split}
	}
	return []string{w}
}

// twoLetterWords are the two-letter English words a run-together name may
// be split into. Most two-letter dictionary entries are abbreviations or
// names, and allowing them would split almost anything; parts of three
// letters or more come from the lexicon alone.
var twoLetterWords = map[string]bool{
	"am": true, "an": true, "as": true, "at": true, "be": true, "by": true,
	"do": true, "go": true, "he": true, "if": true, "in": true, "is": true,
	"it": true, "me": true, "my": true, "no": true, "of": true, "on": true,
	"or": true, "so": true, "to": true, "up": true, "us": true, "we": true,
}

// splitPacked splits words run together ("readme", "keybind", "gitignore")
// into the fewest lexicon words, preferring the split whose shortest part is
// longest. ok is false when there is no split into two or more words.
func splitPacked(w string, lex lexicon) (string, bool) {
	type best struct {
		parts, shortest int
		prev            int
		ok              bool
	}
	n := len(w)
	dp := make([]best, n+1)
	dp[0] = best{ok: true, shortest: n + 1}
	for end := 2; end <= n; end++ {
		for start := 0; start <= end-2; start++ {
			if !dp[start].ok {
				continue
			}
			part := w[start:end]
			if !(len(part) >= 3 && lex.word(part)) && !twoLetterWords[part] {
				continue
			}
			cand := best{parts: dp[start].parts + 1, shortest: min(dp[start].shortest, len(part)), prev: start, ok: true}
			cur := dp[end]
			if !cur.ok || cand.parts < cur.parts || (cand.parts == cur.parts && cand.shortest > cur.shortest) {
				dp[end] = cand
			}
		}
	}
	if !dp[n].ok || dp[n].parts < 2 {
		return "", false
	}
	var parts []string
	for end := n; end > 0; end = dp[end].prev {
		parts = append([]string{w[dp[end].prev:end]}, parts...)
	}
	return strings.Join(parts, " "), true
}

// extensionNames are the extensions people say as the language or format
// they name rather than as their letters. Any other extension goes through
// the ordinary chunk rules ("json", "toml", "html" → "h t m l").
var extensionNames = map[string]string{
	"rs": "rust", "ts": "typescript", "js": "javascript", "mjs": "javascript",
	"py": "python", "md": "markdown", "yml": "yaml", "sh": "shell",
	"txt": "text", "h": "header", "cpp": "c plus plus", "rb": "ruby",
	"kt": "kotlin",
}

// extensionForms is how an extension is said: its language or format name
// when it has one, and its own reading beside it.
func extensionForms(ext string, lex lexicon) []string {
	ext = strings.ToLower(ext)
	var forms []string
	if name, ok := extensionNames[ext]; ok {
		forms = append(forms, name)
	}
	return dedupe(append(forms, pieceForms(ext, lex)...))
}

// numberForms reads a run of digits: naturally ("twelve", "two thousand
// twenty six"), as a year when it looks like one ("twenty twenty six"), and
// digit by digit ("one two"). Leading zeros are only said digit by digit.
func numberForms(digits string) []string {
	var forms []string
	n, err := strconv.Atoi(digits)
	if err == nil && n < 1_000_000 && (digits[0] != '0' || len(digits) == 1) {
		forms = append(forms, cardinal(n))
		if len(digits) == 4 && n >= 1100 && n < 3000 && n%100 != 0 {
			hi, lo := n/100, n%100
			low := cardinal(lo)
			if lo < 10 {
				low = "oh " + low
			}
			forms = append(forms, cardinal(hi)+" "+low)
		}
	}
	if len(digits) > 1 || len(forms) == 0 {
		ds := make([]string, 0, len(digits))
		for _, d := range digits {
			ds = append(ds, ones[d-'0'])
		}
		forms = append(forms, strings.Join(ds, " "))
	}
	return dedupe(forms)
}

var ones = []string{"zero", "one", "two", "three", "four", "five", "six", "seven", "eight", "nine",
	"ten", "eleven", "twelve", "thirteen", "fourteen", "fifteen", "sixteen", "seventeen", "eighteen", "nineteen"}
var tens = []string{"", "", "twenty", "thirty", "forty", "fifty", "sixty", "seventy", "eighty", "ninety"}

// cardinal is n in words, below a million, without "and".
func cardinal(n int) string {
	switch {
	case n < 20:
		return ones[n]
	case n < 100:
		if n%10 == 0 {
			return tens[n/10]
		}
		return tens[n/10] + " " + ones[n%10]
	case n < 1000:
		if n%100 == 0 {
			return ones[n/100] + " hundred"
		}
		return ones[n/100] + " hundred " + cardinal(n%100)
	default:
		if n%1000 == 0 {
			return cardinal(n/1000) + " thousand"
		}
		return cardinal(n/1000) + " thousand " + cardinal(n%1000)
	}
}

// foldLetters lowercases c and reduces it to a-z: accented Latin letters
// lose their accent, and any other letter is dropped, since the engine
// cannot hear it.
func foldLetters(c string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(c) {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		default:
			if f, ok := accentFold[r]; ok {
				b.WriteString(f)
			}
		}
	}
	return b.String()
}

var accentFold = map[rune]string{
	'à': "a", 'á': "a", 'â': "a", 'ã': "a", 'ä': "a", 'å': "a", 'æ': "ae",
	'ç': "c", 'è': "e", 'é': "e", 'ê': "e", 'ë': "e", 'ì': "i", 'í': "i",
	'î': "i", 'ï': "i", 'ñ': "n", 'ò': "o", 'ó': "o", 'ô': "o", 'õ': "o",
	'ö': "o", 'ø': "o", 'œ': "oe", 'ß': "ss", 'ù': "u", 'ú': "u", 'û': "u",
	'ü': "u", 'ý': "y", 'ÿ': "y",
}

func spell(w string) string {
	return strings.Join(strings.Split(w, ""), " ")
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func isUpper(s string) bool {
	hasLetter := false
	for _, r := range s {
		if unicode.IsLower(r) {
			return false
		}
		if unicode.IsLetter(r) {
			hasLetter = true
		}
	}
	return hasLetter
}

func hasVowel(w string) bool {
	return strings.ContainsAny(w, "aeiouy")
}

func dedupe(xs []string) []string {
	seen := make(map[string]bool, len(xs))
	out := xs[:0:0]
	for _, x := range xs {
		if x != "" && !seen[x] {
			seen[x] = true
			out = append(out, x)
		}
	}
	return out
}
