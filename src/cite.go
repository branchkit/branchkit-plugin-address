package main

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/branchkit/plugin-sdk-go"
)

// Saying a name and getting the address typed: one file, typed at once;
// several, a numbered choice the person answers by saying the number; a
// choice answered before, typed at once.

const (
	choicesCollection = "plugin.address.choices"
	choosingTag       = "plugin.address.choosing"
	learnedCollection = "plugin.address.learned"
	hudChannel        = "address"
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

// badges are the words the choices are numbered with. Nine at most, so a
// badge is always one short word.
var badges = []string{"one", "two", "three", "four", "five", "six", "seven", "eight", "nine"}

// cite types the address of the file `spoken` names.
func (h *Host) cite(spoken string) error {
	spoken = strings.TrimSpace(spoken)
	paths, cwd := h.lookup(spoken)
	if len(paths) == 0 {
		// A name the person added in the Collections page is in the
		// collection but not in what this plugin published: read it back.
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
	h.mu.Lock()
	c := h.choosing
	h.mu.Unlock()
	if c != nil && slices.Contains(c.paths, path) {
		h.learn(c.cwd, c.spoken, path)
	}
	h.closeChoices()
	return nil
}

// offer shows the files an ambiguous name could mean, numbered, and makes
// the numbers the only thing heard until one is said, "cancel" is said, or
// the hold ends.
func (h *Host) offer(c choice) error {
	if len(c.paths) > len(badges) {
		c.paths = c.paths[:len(badges)]
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
		items = append(items, branchkit.OutputItem{
			ID:     badges[i],
			Phrase: badges[i],
			Title:  fmt.Sprintf("%d  %s", i+1, p),
			Action: &branchkit.OutputAction{Dispatch: &dispatch, Params: params},
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
	if err := h.plugin.Put(choosingTag, "singleton", struct{}{}); err != nil {
		h.abandon(&c)
		return fmt.Errorf("enter choosing: %w", err)
	}

	footer := "say a number, or cancel"
	doc := branchkit.OutputState{
		Channel:  hudChannel,
		Kind:     branchkit.OutputKindChoices,
		Title:    "Which file?",
		Phrase:   fmt.Sprintf("%q could be %s. Say a number.", c.spoken, countFiles(len(c.paths))),
		Sections: []branchkit.OutputSection{{Items: items}},
		Footer:   &footer,
		Urgency:  branchkit.OutputUrgencyNotable,
		Locale:   "en",
		V:        1,
	}
	if _, err := h.plugin.OutputState(branchkit.OutputStateRequest{State: doc}); err != nil {
		branchkit.Logf(pluginID, "choices output.state: %v", err)
	}
	if err := h.plugin.HUDShow(branchkit.HUDShowRequest{Channel: hudChannel}); err != nil {
		branchkit.Logf(pluginID, "choices show: %v", err)
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
	if n < len(badges)+1 && n > 0 {
		return badges[n-1] + " files"
	}
	return fmt.Sprintf("%d files", n)
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
	if _, err := h.plugin.Delete(choosingTag, "singleton"); err != nil {
		branchkit.Logf(pluginID, "leave choosing: %v", err)
	}
	if _, err := h.plugin.Replace(choicesCollection, nil, branchkit.ScopeCollection()); err != nil {
		branchkit.Logf(pluginID, "clear choices: %v", err)
	}
	if _, err := h.plugin.OutputClear(branchkit.OutputClearRequest{Channel: hudChannel}); err != nil {
		branchkit.Logf(pluginID, "choices clear: %v", err)
	}
	if err := h.plugin.HUDHide(branchkit.HUDHideRequest{Channel: hudChannel}); err != nil {
		branchkit.Logf(pluginID, "choices hide: %v", err)
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
