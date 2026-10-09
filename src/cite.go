package main

import (
	"encoding/json"
	"fmt"
	"path"
	"slices"
	"sort"
	"strings"

	"github.com/branchkit/plugin-sdk-go"
)

// Saying a name and getting the address typed: one file, typed at once;
// several, a numbered choice the person answers by saying the number; a
// choice answered before, typed at once.

const (
	choicesCollection = "plugin.address.choices"
	choosingTag       = "plugin.address.choosing"
	browsingTag       = "plugin.address.browsing"
	learnedCollection = "plugin.address.learned"
	// alphabetCollection is the platform's pointing deck: one vetted
	// codeword per letter, the words every chooser badges its rows with.
	alphabetCollection = "alphabet"
	hudChannel         = "address"
)

// choice is an ambiguous name being chosen between.
type choice struct {
	cwd    string
	spoken string
	paths  []string
}

// learnedRecord is one answer to "which file?".
type learnedRecord struct {
	Cwd    string `json:"cwd"`
	Spoken string `json:"spoken"`
	Path   string `json:"path"`
}

func learnKey(cwd, spoken string) string { return cwd + "\x00" + spoken }

// learnedID is the record id of an answer: the project folder and the name,
// which is what an answer is unique by.
func learnedID(cwd, spoken string) string { return cwd + "#" + spoken }

// mention is a file's address as Claude Code takes it: `@path` and a space,
// so the next word is not read as part of the path. Claude Code is the only
// destination so far; other destinations write an address differently
// (a Markdown link, `[[note]]`), and will each be a format chosen by the
// focused app.
func mention(path string) string { return "@" + path + " " }

// numberWords badge the choices when no alphabet is published (it is the
// voice plugin's), and count them in what the window says.
var numberWords = []string{"one", "two", "three", "four", "five", "six", "seven", "eight", "nine"}

// deck is the words the choices are badged with: the alphabet's codewords,
// sorted as the platform's own choosers sort them, so a badge means the
// same word here as in every other list the person points into.
func (h *Host) deck() []string {
	records, err := h.plugin.ListAll(alphabetCollection)
	if err != nil {
		branchkit.Logf(pluginID, "read %s: %v (badging with numbers)", alphabetCollection, err)
	}
	seen := map[string]bool{}
	var words []string
	for _, rec := range records {
		var r AlphabetRecord
		w := ""
		if json.Unmarshal(rec.Payload, &r) == nil {
			w = strings.ToLower(strings.TrimSpace(r.Codeword))
		}
		if w != "" && !strings.Contains(w, " ") && !seen[w] {
			seen[w] = true
			words = append(words, w)
		}
	}
	if len(words) < maxChoices {
		return numberWords
	}
	sort.Strings(words)
	return words
}

// incidentalDirs hold files that share a name with the file a person means
// without being it: tests, fixtures, vendored and installed code.
var incidentalDirs = map[string]bool{
	"test": true, "tests": true, "__tests__": true, "testdata": true,
	"fixtures": true, "test-fixtures": true, "test_fixtures": true, "__fixtures__": true,
	"mocks": true, "__mocks__": true,
	"vendor": true, "third_party": true, "node_modules": true,
}

func incidental(p string) bool {
	for _, dir := range strings.Split(path.Dir(p), "/") {
		if incidentalDirs[strings.ToLower(dir)] {
			return true
		}
	}
	return false
}

// clearWinner is the one file a name means when every other file it could
// mean is incidental: "mission" is docs/MISSION.md, not a test fixture
// called mission.html. The others keep their longer names, so they are
// still sayable; only a genuine choice is asked.
func clearWinner(paths []string) (string, bool) {
	winner := ""
	for _, p := range paths {
		if incidental(p) {
			continue
		}
		if winner != "" {
			return "", false
		}
		winner = p
	}
	return winner, winner != ""
}

// maxBrowse is the most files "mention" alone lists: one alphabet word
// each, so every badge is a single word.
const maxBrowse = 26

// browse lists the files the person most likely means, badged, for
// "mention" said alone: the files the working tree has changed, then the
// ones mentioned most recently. While the list is open a file's name works
// too.
func (h *Host) browse() error {
	h.mu.Lock()
	cwd := h.project.cwd
	paths := likelyFiles(h.project.changed, h.recentIn(cwd), maxBrowse)
	h.mu.Unlock()
	return h.offer(choice{cwd: cwd, paths: paths})
}

// likelyFiles is changed then recent, each file once, at most n.
func likelyFiles(changed, recent []string, n int) []string {
	seen := map[string]bool{}
	var out []string
	for _, list := range [][]string{changed, recent} {
		for _, p := range list {
			if len(out) == n {
				return out
			}
			if !seen[p] {
				seen[p] = true
				out = append(out, p)
			}
		}
	}
	return out
}

// recentIn is the recent files when they belong to project cwd. Caller
// holds h.mu.
func (h *Host) recentIn(cwd string) []string {
	if h.recentCwd != cwd {
		return nil
	}
	return h.recent
}

// noteRecent puts a typed file at the head of the recent list.
func (h *Host) noteRecent(path string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.recentCwd != h.project.cwd {
		h.recentCwd, h.recent = h.project.cwd, nil
	}
	list := []string{path}
	for _, p := range h.recent {
		if p != path && len(list) < maxBrowse {
			list = append(list, p)
		}
	}
	h.recent = list
}

// cite types the address of the file `spoken` names.
func (h *Host) cite(spoken string) error {
	spoken = strings.TrimSpace(spoken)
	paths, cwd := h.lookup(spoken)
	if len(paths) == 0 {
		// A name something else wrote to the collection (a script, the
		// CLI) is not in what this plugin published: read it back.
		if err := h.loadProject(); err != nil {
			branchkit.Logf(pluginID, "cite %q: %v", spoken, err)
		}
		paths, cwd = h.lookup(spoken)
	}
	switch {
	case len(paths) == 0:
		return fmt.Errorf("no file in this project is called %q", spoken)
	case len(paths) == 1:
		return h.insert(paths[0])
	}
	// Read fresh: the person may have deleted an answer to be asked again.
	if err := h.loadLearned(); err != nil {
		branchkit.Logf(pluginID, "cite %q: %v", spoken, err)
	}
	h.mu.Lock()
	picked := h.learned[learnKey(cwd, spoken)]
	h.mu.Unlock()
	for _, p := range paths {
		if p == picked {
			return h.insert(p)
		}
	}
	if p, ok := clearWinner(paths); ok {
		return h.insert(p)
	}
	return h.offer(choice{cwd: cwd, spoken: spoken, paths: paths})
}

func (h *Host) lookup(spoken string) ([]string, string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.project.names[spoken], h.project.cwd
}

// insert types a file's address. When it answers an open choice, the answer
// is remembered: that name in that project means this file from now on.
func (h *Host) insert(path string) error {
	if !mentionable(path) {
		return fmt.Errorf("%q is not a path inside the project", path)
	}
	if err := h.plugin.InputTypeText(branchkit.InputTypeTextRequest{Text: mention(path)}); err != nil {
		return fmt.Errorf("type @%s: %w", path, err)
	}
	h.noteRecent(path)
	h.mu.Lock()
	c := h.choosing
	h.mu.Unlock()
	// A pick from the list "mention" alone shows answers no name, so it
	// teaches none.
	if c != nil && c.spoken != "" && slices.Contains(c.paths, path) {
		h.learn(c.cwd, c.spoken, path)
	}
	h.closeChoices()
	return nil
}

// offer shows a list of files, each beside a badge word. For an ambiguous
// name the badges are the only thing heard until one is said, "cancel" is
// said, or the hold ends; for the list "mention" opens they are heard
// beside everything else until a pick or a close.
func (h *Host) offer(c choice) error {
	badges := h.deck()
	limit := maxChoices
	if c.spoken == "" {
		limit = maxBrowse
	}
	if n := min(len(badges), limit); len(c.paths) > n {
		c.paths = c.paths[:n]
	}
	entries := make([]branchkit.CollectionPutEntry, 0, len(c.paths))
	items := make([]branchkit.OutputItem, 0, len(c.paths))
	for i, p := range c.paths {
		raw, err := json.Marshal(map[string]string{"codeword": badges[i], "path": p})
		if err != nil {
			return err
		}
		entries = append(entries, branchkit.CollectionPutEntry{ID: badges[i], Payload: raw})
		params, err := json.Marshal(InsertParams{Path: p})
		if err != nil {
			return err
		}
		dispatch := "address.insert"
		file := p
		// The word to say is the title, the file beneath it: how the
		// platform's choosers lay out a badged row.
		items = append(items, branchkit.OutputItem{
			ID:       badges[i],
			Phrase:   badges[i],
			Title:    badges[i],
			Subtitle: &file,
			Action:   &branchkit.OutputAction{Dispatch: &dispatch, Params: params},
		})
	}

	// The choice is recorded before anything is written, so a hold that
	// ends while it is being written (the name decodes at the release, so
	// the action and the hold's end arrive together) finds it open and
	// closes it, rather than finding nothing and leaving the gate behind.
	h.mu.Lock()
	h.choosing = &c
	h.mu.Unlock()

	// The choices first, then the gate that makes them the only thing
	// heard: the other order would open a mode with nothing in it.
	if _, err := h.plugin.Replace(choicesCollection, entries, branchkit.ScopeCollection()); err != nil {
		h.abandon(&c)
		return fmt.Errorf("publish choices: %w", err)
	}
	// "Which file?" is exclusive and lasts the hold; the list "mention"
	// opens adds to what can be said, in every hold until it closes.
	gate := choosingTag
	if c.spoken == "" {
		gate = browsingTag
	}
	if err := h.plugin.Put(gate, "singleton", struct{}{}); err != nil {
		h.abandon(&c)
		return fmt.Errorf("enter choosing: %w", err)
	}

	footer := "say the word beside a file, or cancel"
	phrase := fmt.Sprintf("%q could be %s. Say the word beside the one you mean.", c.spoken, countFiles(len(c.paths)))
	if c.spoken == "" {
		footer = "say the word beside a file, or cancel; or pick one"
		phrase = "Files you changed or mentioned. Say the word beside one, or pick it."
		if len(c.paths) == 0 {
			phrase = "No changed or recently mentioned files yet. Say \"mention\" and a file's name."
		}
	}
	// An empty list is no section: the window shows the phrase instead.
	var sections []branchkit.OutputSection
	if len(items) > 0 {
		sections = []branchkit.OutputSection{{Items: items}}
	}
	doc := branchkit.OutputState{
		Channel:  hudChannel,
		Kind:     branchkit.OutputKindChoices,
		Title:    "Which file?",
		Phrase:   phrase,
		Sections: sections,
		Footer:   &footer,
		Urgency:  branchkit.OutputUrgencyNotable,
		Locale:   "en",
		V:        1,
	}
	if _, err := h.plugin.OutputState(branchkit.OutputStateRequest{State: doc}); err != nil {
		branchkit.Logf(pluginID, "choices output.state: %v", err)
	}
	// Closed while it was being written: the close cleared what existed
	// then, and the writes since put the gate and the window back. Clear
	// them again.
	if !h.isChoosing(&c) {
		h.clearChoices()
	}
	return nil
}

func (h *Host) isChoosing(c *choice) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.choosing == c
}

// abandon closes a choice that failed to open, unless something else has
// already closed it.
func (h *Host) abandon(c *choice) {
	h.mu.Lock()
	mine := h.choosing == c
	if mine {
		h.choosing = nil
	}
	h.mu.Unlock()
	h.clearChoices()
}

func countFiles(n int) string {
	if n < len(numberWords)+1 && n > 0 {
		return numberWords[n-1] + " files"
	}
	return fmt.Sprintf("%d files", n)
}

// holdEnded closes a choice between the files a name could mean; a list
// "mention" alone opened stays on screen until a pick or its close.
func (h *Host) holdEnded() {
	h.mu.Lock()
	browsing := h.choosing != nil && h.choosing.spoken == ""
	h.mu.Unlock()
	if !browsing {
		h.closeChoices()
	}
}

// closeChoices ends a choice, if one is open: the gate, the badges and the
// window. Each step is idempotent, so an end that the hold's own clearing
// already half-did is harmless.
func (h *Host) closeChoices() {
	h.mu.Lock()
	open := h.choosing != nil
	h.choosing = nil
	h.mu.Unlock()
	if !open {
		return
	}
	h.clearChoices()
}

// clearChoices removes the gate, the badges and the window.
func (h *Host) clearChoices() {
	for _, gate := range []string{choosingTag, browsingTag} {
		if _, err := h.plugin.Delete(gate, "singleton"); err != nil {
			branchkit.Logf(pluginID, "leave %s: %v", gate, err)
		}
	}
	if _, err := h.plugin.Replace(choicesCollection, nil, branchkit.ScopeCollection()); err != nil {
		branchkit.Logf(pluginID, "clear choices: %v", err)
	}
	if _, err := h.plugin.OutputClear(branchkit.OutputClearRequest{Channel: hudChannel}); err != nil {
		branchkit.Logf(pluginID, "choices clear: %v", err)
	}
}

// learn records the person's answer, so the name means that file in that
// project from now on. The record is the person's to see and delete.
func (h *Host) learn(cwd, spoken, path string) {
	h.mu.Lock()
	h.learned[learnKey(cwd, spoken)] = path
	h.mu.Unlock()
	rec := learnedRecord{Cwd: cwd, Spoken: spoken, Path: path}
	if err := h.plugin.Put(learnedCollection, learnedID(cwd, spoken), rec); err != nil {
		branchkit.Logf(pluginID, "learn %q: %v", spoken, err)
	}
}

// loadLearned reads the person's answers, including any they deleted since
// (gone from the collection, so gone here).
func (h *Host) loadLearned() error {
	records, err := h.plugin.ListAll(learnedCollection)
	if err != nil {
		return fmt.Errorf("read %s: %w", learnedCollection, err)
	}
	learned := map[string]string{}
	for _, rec := range records {
		var r learnedRecord
		if err := json.Unmarshal(rec.Payload, &r); err != nil || r.Spoken == "" || r.Path == "" {
			continue
		}
		learned[learnKey(r.Cwd, r.Spoken)] = r.Path
	}
	h.mu.Lock()
	h.learned = learned
	h.mu.Unlock()
	return nil
}
