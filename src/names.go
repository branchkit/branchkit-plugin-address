package main

import (
	"path"
	"sort"
	"strings"
)

// How a set of project files gets its spoken names. One file's own words come
// from words.go; this decides which of the ways to say a file are its names,
// given every other file in the project.
//
// The model is VS Code's labels for editor tabs with the same file name: a
// file is named by the shortest end of its path that no other file shares
// ("names" alone, or "src names", or "address src names"). Its whole path
// is always a name too, so saying more than needed still works. A name two
// files share is published as ambiguous, listing both, and the person picks:
// the plugin never chooses between them silently.

// maxChoices is the most files one ambiguous name may offer: one badge each,
// numbered one to nine. A name shared more widely ("src mod" in a large
// project) is no help to say, so it is not a name.
const maxChoices = 9

// maxLevelForms caps the ways to say one end of a path (folders × file name
// × extension alternatives).
const maxLevelForms = 8

// genericStems are file names that say what kind of file it is rather than
// which one ("mod", "main", "README"). Bare, they would name a file in only
// the smallest project, so they are always said with their folder ("sandbox
// mod"), even when they happen to be unique.
var genericStems = map[string]bool{
	"mod": true, "main": true, "index": true, "lib": true, "init": true,
	"read me": true, "readme": true, "license": true, "change log": true,
	"changelog": true, "make file": true, "makefile": true, "docker file": true,
	"dockerfile": true, "cargo": true, "package": true, "go": true,
	"plugin": true, "types": true, "utils": true, "util": true, "test": true,
	"tests": true, "setup": true, "app": true, "config": true,
	"settings": true, "default": true, "defaults": true, "common": true,
	"helpers": true, "constants": true, "manifest": true, "build": true,
	"build rs": true, "build r s": true, "commands": true, "go mod": true,
}

// partialStopwords are words too general to name a file on their own even
// when only one file contains them.
var partialStopwords = map[string]bool{
	"test": true, "tests": true, "spec": true, "data": true, "file": true,
	"files": true, "type": true, "types": true, "util": true, "utils": true,
	"helper": true, "helpers": true, "main": true, "index": true,
	"common": true, "base": true, "core": true, "impl": true, "with": true,
	"from": true, "into": true, "this": true, "that": true, "gen": true,
}

// nameTable maps each spoken name to the files it could mean, most likely
// first. One file: the name is that file's. Several: the name is ambiguous
// and the person chooses.
type nameTable map[string][]string

type fileForms struct {
	path string
	// bare is the file name alone, extension left off; nil when the name is
	// generic and must be said with a folder.
	bare []string
	// withFolder[k] is the file name after one of its folders, the nearest
	// first ("voice main" for plugins/voice/src/main.go).
	withFolder [][]string
	// tails[k] is the file name after its k+2 nearest folders in order
	// ("voice s r c main"). The last tail, or the last withFolder or bare
	// when there are fewer folders, is the whole path.
	tails [][]string
	// withExt is the whole path said with the extension.
	withExt []string
	// stem is the file name's own words, for single-word names.
	stem []string
}

// whole is the file's whole path, said without its extension.
func (f fileForms) whole() []string {
	switch {
	case len(f.tails) > 0:
		return f.tails[len(f.tails)-1]
	case len(f.withFolder) > 0:
		return f.withFolder[0]
	}
	return f.bare
}

// short is every form short enough to be worth offering as a choice when
// several files share it: the name alone, or after one folder.
func (f fileForms) short() [][]string {
	return append([][]string{f.bare}, f.withFolder...)
}

// candidates is every way the file may be named, in the order names are
// tried: the name alone, after one folder (nearest first), after its
// nearest folders in order, and the whole path with its extension last.
func (f fileForms) candidates() [][]string {
	var out [][]string
	if f.bare != nil {
		out = append(out, f.bare)
	}
	out = append(out, f.withFolder...)
	out = append(out, f.tails...)
	if len(f.withExt) > 0 {
		out = append(out, f.withExt)
	}
	return out
}

// rankFiles orders files most likely to be meant first: files changed in the
// working tree, then files nearer the project root, then by path.
func rankFiles(paths []string, changed map[string]bool) []string {
	out := append([]string(nil), paths...)
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if changed[a] != changed[b] {
			return changed[a]
		}
		if da, db := strings.Count(a, "/"), strings.Count(b, "/"); da != db {
			return da < db
		}
		return a < b
	})
	return out
}

// splitName separates a file name into its name and extension: "DEV_LOOP.md"
// → "DEV_LOOP", "md"; ".gitignore" → "gitignore", ""; "foo.test.ts" →
// "foo.test", "ts".
func splitName(base string) (stem, ext string) {
	b := strings.TrimLeft(base, ".")
	if i := strings.LastIndexByte(b, '.'); i > 0 {
		return b[:i], b[i+1:]
	}
	return b, ""
}

// formsOf works out every way to say one file, level by level.
func formsOf(p string, lex lexicon) (fileForms, bool) {
	dir, base := path.Split(p)
	stem, ext := splitName(base)
	stemForms := pieceForms(stem, lex)
	extForms := []string(nil)
	if ext != "" {
		extForms = extensionForms(ext, lex)
	}
	if len(stemForms) == 0 {
		// Nothing sayable in the name itself ("_.go"): the extension is all
		// there is to call it.
		stemForms, extForms = extForms, nil
	}
	if len(stemForms) == 0 {
		return fileForms{}, false
	}

	var dirForms [][]string
	for _, d := range strings.Split(strings.Trim(dir, "/"), "/") {
		if f := pieceForms(d, lex); len(f) > 0 {
			dirForms = append(dirForms, f)
		}
	}

	ff := fileForms{path: p, stem: strings.Fields(stemForms[0])}
	if !genericStems[stemForms[0]] || len(dirForms) == 0 {
		ff.bare = stemForms
	}
	for k := len(dirForms) - 1; k >= 0; k-- {
		ff.withFolder = append(ff.withFolder, product(dirForms[k], stemForms))
	}
	for k := 2; k <= len(dirForms); k++ {
		tail := []string{""}
		for _, d := range dirForms[len(dirForms)-k:] {
			tail = product(tail, d)
		}
		ff.tails = append(ff.tails, product(tail, stemForms))
	}
	for _, w := range ff.whole() {
		for _, e := range extForms {
			ff.withExt = append(ff.withExt, w+" "+e)
		}
	}
	ff.withExt = dedupe(ff.withExt)
	if len(ff.withExt) > maxLevelForms {
		ff.withExt = ff.withExt[:maxLevelForms]
	}
	return ff, true
}

func product(prefixes, words []string) []string {
	out := make([]string, 0, len(prefixes)*len(words))
	for _, p := range prefixes {
		for _, w := range words {
			out = append(out, strings.TrimSpace(p+" "+w))
		}
	}
	if len(out) > maxLevelForms {
		out = out[:maxLevelForms]
	}
	return out
}

// nameFiles names every file in paths (project-relative, any order), with
// changed marking the files the working tree has changed.
func nameFiles(paths []string, changed map[string]bool, lex lexicon) nameTable {
	ranked := rankFiles(paths, changed)
	var files []fileForms
	for _, p := range ranked {
		if ff, ok := formsOf(p, lex); ok {
			files = append(files, ff)
		}
	}

	// Who could each spoken string mean, and whose whole path is it.
	claims := map[string][]int{}
	whole := map[string][]int{}
	claim := func(m map[string][]int, s string, i int) {
		if c := m[s]; len(c) == 0 || c[len(c)-1] != i {
			m[s] = append(c, i)
		}
	}
	for i, f := range files {
		for _, level := range f.candidates() {
			for _, s := range level {
				claim(claims, s, i)
			}
		}
		for _, s := range f.whole() {
			claim(whole, s, i)
		}
	}
	// owner is the one file a string names, or -1. A string two files claim
	// still belongs to one of them when it is that file's whole path: "a x"
	// is a/x.go's, even though b/a/x.go also ends in "a x".
	owner := func(s string) int {
		if c := claims[s]; len(c) == 1 {
			return c[0]
		}
		if w := whole[s]; len(w) == 1 {
			return w[0]
		}
		return -1
	}
	ambiguous := func(table nameTable, s string) {
		if table[s] != nil || len(claims[s]) > maxChoices {
			return
		}
		var paths []string
		for _, j := range claims[s] {
			paths = append(paths, files[j].path)
		}
		table[s] = paths
	}

	table := nameTable{}
	for i, f := range files {
		// The shortest end of the path no other file shares.
		named := false
		for _, level := range f.candidates() {
			for _, s := range level {
				if owner(s) == i {
					table[s] = []string{f.path}
					named = true
				}
			}
			if named {
				break
			}
		}
		// Nothing about it is its own (two files whose paths read alike,
		// "foo_bar.md" and "foo-bar.md"): its fullest names are ambiguous.
		if !named {
			ends := f.candidates()
			for _, s := range ends[len(ends)-1] {
				ambiguous(table, s)
			}
		}
	}

	// A short name several files share is ambiguous: say it and choose
	// ("mention voice main", then which). Files are listed most likely first,
	// since claims were made in rank order. A generic name ("main") is never
	// said alone, so it is never offered alone either.
	for _, f := range files {
		for _, level := range f.short() {
			for _, s := range level {
				if owner(s) == -1 {
					ambiguous(table, s)
				}
			}
		}
	}

	// One word of a file name, when no other file's name has it and it is
	// not a word that could mean anything ("matching" for
	// matching_service.rs).
	partial := map[string][]int{}
	for i, f := range files {
		if len(f.stem) < 2 {
			continue
		}
		for _, w := range f.stem {
			if len(w) >= 4 && !genericStems[w] && !partialStopwords[w] {
				claim(partial, w, i)
			}
		}
	}
	for w, c := range partial {
		if len(c) == 1 && len(claims[w]) == 0 && table[w] == nil {
			table[w] = []string{files[c[0]].path}
		}
	}
	return table
}
