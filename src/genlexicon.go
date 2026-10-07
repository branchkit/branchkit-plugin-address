//go:build ignore

// genlexicon writes lexicon.txt.gz, the English word list the naming rules
// consult, from the CMU Pronouncing Dictionary:
//
//	go run genlexicon.go path/to/cmudict.dict.gz
//
// The list answers one question: is this chunk of a file name a word a
// person says as a word? So it keeps every plain lowercase entry and drops
// the entries whose only pronunciations spell the word out letter by letter
// ("api" is "A P I", "md" is "M D"): those are abbreviations, and a name
// built from one is said as its letters, which the naming rules do on their
// own.
//
// The input is cmudict.dict from https://github.com/cmusphinx/cmudict at
// commit 74790861f652b15e4ac49015a90074ad62a27690, gzipped. CMUdict is
// BSD-licensed by Carnegie Mellon University; its notice is in
// THIRD_PARTY_NOTICES.md.
package main

import (
	"bufio"
	"compress/gzip"
	"fmt"
	"os"
	"sort"
	"strings"
)

// letterNames is how each letter is pronounced on its own, in ARPAbet without
// stress marks. W has two common readings.
var letterNames = map[byte][]string{
	'a': {"EY"}, 'b': {"B IY"}, 'c': {"S IY"}, 'd': {"D IY"}, 'e': {"IY"},
	'f': {"EH F"}, 'g': {"JH IY"}, 'h': {"EY CH"}, 'i': {"AY"}, 'j': {"JH EY"},
	'k': {"K EY"}, 'l': {"EH L"}, 'm': {"EH M"}, 'n': {"EH N"}, 'o': {"OW"},
	'p': {"P IY"}, 'q': {"K Y UW"}, 'r': {"AA R"}, 's': {"EH S"}, 't': {"T IY"},
	'u': {"Y UW"}, 'v': {"V IY"}, 'w': {"D AH B AH L Y UW", "D AH B AH Y UW"},
	'x': {"EH K S"}, 'y': {"W AY"}, 'z': {"Z IY"},
}

// spelled reports whether phones is word read out as its letters.
func spelled(word string, phones string) bool {
	var match func(i int, rest string) bool
	match = func(i int, rest string) bool {
		if i == len(word) {
			return rest == ""
		}
		for _, name := range letterNames[word[i]] {
			if rest == name {
				if i == len(word)-1 {
					return true
				}
				continue
			}
			if strings.HasPrefix(rest, name+" ") && match(i+1, rest[len(name)+1:]) {
				return true
			}
		}
		return false
	}
	return match(0, phones)
}

func stripStress(phones []string) string {
	out := make([]string, len(phones))
	for i, p := range phones {
		out[i] = strings.TrimRight(p, "012")
	}
	return strings.Join(out, " ")
}

func plain(word string) bool {
	for i := 0; i < len(word); i++ {
		if word[i] < 'a' || word[i] > 'z' {
			return false
		}
	}
	return word != ""
}

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: go run genlexicon.go cmudict.dict.gz")
		os.Exit(2)
	}
	f, err := os.Open(os.Args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer f.Close()
	zr, err := gzip.NewReader(f)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	// word -> whether at least one pronunciation is NOT spelled out.
	sayable := map[string]bool{}
	sc := bufio.NewScanner(zr)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) < 2 || strings.HasPrefix(fields[0], ";;;") {
			continue
		}
		word := fields[0]
		if i := strings.IndexByte(word, '('); i > 0 {
			word = word[:i] // "read(2)": an alternative pronunciation
		}
		if !plain(word) {
			continue
		}
		phones := fields[1:]
		if i := indexOf(phones, "#"); i >= 0 {
			phones = phones[:i] // trailing comment
		}
		if !spelled(word, stripStress(phones)) {
			sayable[word] = true
		} else if _, seen := sayable[word]; !seen {
			sayable[word] = false
		}
	}
	if err := sc.Err(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	var words []string
	for w, ok := range sayable {
		// One-letter entries are letters, not words, except the two that
		// are both.
		if ok && (len(w) > 1 || w == "a" || w == "i") {
			words = append(words, w)
		}
	}
	sort.Strings(words)

	out, err := os.Create("lexicon.txt.gz")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	zw, _ := gzip.NewWriterLevel(out, gzip.BestCompression)
	// A fixed header keeps the output byte-identical run to run.
	zw.ModTime = zw.ModTime.UTC()
	zw.Name = ""
	for _, w := range words {
		fmt.Fprintln(zw, w)
	}
	if err := zw.Close(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := out.Close(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "lexicon.txt.gz: %d words\n", len(words))
}

func indexOf(xs []string, s string) int {
	for i, x := range xs {
		if x == s {
			return i
		}
	}
	return -1
}
