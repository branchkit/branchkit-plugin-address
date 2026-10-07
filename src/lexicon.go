package main

import (
	"bufio"
	"bytes"
	"compress/gzip"
	_ "embed"
	"sync"
)

// lexicon.txt.gz is every plain English word in the CMU Pronouncing
// Dictionary that is said as a word rather than spelled out (genlexicon.go
// builds it; regenerate it there, never by hand).
//
//go:embed lexicon.txt.gz
var lexiconGz []byte

// lexicon answers whether a lowercase chunk is a word a person says as a
// word. The speech engine itself has no word list (it spells any lowercase
// word in sub-word pieces), so this is the only place "is it a word" can be
// asked.
type lexicon interface {
	word(w string) bool
}

type wordSet map[string]struct{}

func (s wordSet) word(w string) bool {
	_, ok := s[w]
	return ok
}

var (
	englishOnce sync.Once
	english     wordSet
)

// englishLexicon is the embedded list, decoded once on first use (~120k
// words, a few MB in memory).
func englishLexicon() lexicon {
	englishOnce.Do(func() {
		english = wordSet{}
		zr, err := gzip.NewReader(bytes.NewReader(lexiconGz))
		if err != nil {
			panic("address: embedded lexicon is corrupt: " + err.Error())
		}
		sc := bufio.NewScanner(zr)
		for sc.Scan() {
			english[sc.Text()] = struct{}{}
		}
		if err := sc.Err(); err != nil {
			panic("address: embedded lexicon is corrupt: " + err.Error())
		}
	})
	return english
}
