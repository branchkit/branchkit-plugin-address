package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/branchkit/plugin-sdk-go"
)

// The project the person is working in, as the Claude Code hook reports it,
// and its publication as the collection of names the matcher listens for.
//
// The plugin never reads the disk. The hook runs in Claude Code's process,
// lists the project's files with git, and sends the list here; this side only
// derives names from it. So the plugin needs no filesystem grant at all.

const filesCollection = "address_files"

// maxFiles caps how many files one project contributes. Every name is a word
// sequence the speech engine holds while "mention" is active; past a few
// thousand files the names of the least likely ones are not worth the
// recognition they dilute. The most likely files are kept (rankFiles).
const maxFiles = 5000

// maxReportBytes bounds a report's body. The hook sends at most maxFiles
// paths (capReport), each a few hundred bytes at most.
const maxReportBytes = 16 << 20

// projectReport is what the hook sends: the folder Claude Code runs in and
// the files under it, relative to it, which is also how `@path` resolves.
type projectReport struct {
	Cwd       string   `json:"cwd"`
	SessionID string   `json:"session_id,omitempty"`
	Event     string   `json:"event,omitempty"`
	Files     []string `json:"files"`
	// Changed is the files the working tree has changed or added, which
	// rank first.
	Changed []string `json:"changed,omitempty"`
}

// project is one published project: its folder and its names.
type project struct {
	cwd   string
	names nameTable
	// changed is the files the working tree has changed or added, most
	// likely first: the head of the list "mention" alone shows.
	changed []string
}

// fileRecord is one spoken name in the files collection.
type fileRecord struct {
	Spoken string   `json:"spoken"`
	Paths  []string `json:"paths"`
	Cwd    string   `json:"cwd"`
}

// mentionable reports whether a path can be written as a Claude Code
// mention: relative, inside the folder, and free of whitespace, which ends
// an `@` mention.
func mentionable(p string) bool {
	if p == "" || strings.ContainsAny(p, " \t\r\n") || path.IsAbs(p) || filepath.IsAbs(p) {
		return false
	}
	clean := path.Clean(p)
	return clean == p && clean != "." && clean != ".." && !strings.HasPrefix(clean, "../")
}

// handleProject receives a report from the hook. It answers at once and
// publishes in the background, so the hook (and the prompt it runs before)
// never waits on naming.
func (h *Host) handleProject(w http.ResponseWriter, r *http.Request) {
	var rep projectReport
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxReportBytes)).Decode(&rep); err != nil {
		http.Error(w, "bad report: "+err.Error(), http.StatusBadRequest)
		return
	}
	if !filepath.IsAbs(rep.Cwd) {
		http.Error(w, "bad report: cwd must be an absolute path", http.StatusBadRequest)
		return
	}
	h.mu.Lock()
	first := h.heard.IsZero()
	h.heard = time.Now()
	h.mu.Unlock()
	h.submit(rep)
	w.WriteHeader(http.StatusAccepted)
	// The first report turns the Claude Code tab from "not set up" to
	// connected; later ones redraw it when they publish.
	if first {
		h.refreshSettings()
	}
}

// submit queues a report for publishing. Only the newest waiting report is
// kept: reports describe the present, so a newer one makes an older one that
// has not been published yet worthless.
func (h *Host) submit(rep projectReport) {
	h.queueMu.Lock()
	h.queued = &rep
	if h.publishing {
		h.queueMu.Unlock()
		return
	}
	h.publishing = true
	h.queueMu.Unlock()
	go h.drain()
}

func (h *Host) drain() {
	for {
		h.queueMu.Lock()
		rep := h.queued
		h.queued = nil
		if rep == nil {
			h.publishing = false
			h.queueMu.Unlock()
			return
		}
		h.queueMu.Unlock()
		if err := h.publish(*rep); err != nil {
			branchkit.Logf(pluginID, "publish %s: %v", rep.Cwd, err)
		}
	}
}

// publish names a project's files and makes those names what "mention" hears.
// A report identical to the last one published is skipped: the hook reports
// on every prompt, and most prompts change nothing.
func (h *Host) publish(rep projectReport) error {
	files := make([]string, 0, len(rep.Files))
	seen := map[string]bool{}
	for _, f := range rep.Files {
		if mentionable(f) && !seen[f] {
			seen[f] = true
			files = append(files, f)
		}
	}
	changed := map[string]bool{}
	for _, f := range rep.Changed {
		if seen[f] {
			changed[f] = true
		}
	}

	digest := reportDigest(rep.Cwd, files, changed)
	h.queueMu.Lock()
	same := digest == h.lastDigest
	h.queueMu.Unlock()
	if same {
		return nil
	}

	ranked := rankFiles(files, changed)
	if len(ranked) > maxFiles {
		ranked = ranked[:maxFiles]
	}
	names := nameFiles(ranked, changed, h.lex)
	entries, err := fileEntries(rep.Cwd, names)
	if err != nil {
		return err
	}
	if _, err := h.plugin.Replace(filesCollection, entries, branchkit.ScopeCollection()); err != nil {
		return fmt.Errorf("replace %s: %w", filesCollection, err)
	}

	h.mu.Lock()
	var likely []string
	for _, f := range ranked {
		if changed[f] {
			likely = append(likely, f)
		}
	}
	h.project = project{cwd: rep.Cwd, names: names, changed: likely}
	h.published++
	h.mu.Unlock()
	h.queueMu.Lock()
	h.lastDigest = digest
	h.queueMu.Unlock()
	h.saveLately()
	branchkit.Logf(pluginID, "published %d names for %d files in %s", len(names), len(ranked), rep.Cwd)
	h.refreshSettings()
	return nil
}

// refreshSettings redraws an open Claude Code tab, which shows the project
// and when it was reported.
func (h *Host) refreshSettings() {
	if err := h.plugin.SettingsRefresh(); err != nil {
		branchkit.Logf(pluginID, "refresh settings: %v", err)
	}
}

func fileEntries(cwd string, names nameTable) ([]branchkit.CollectionPutEntry, error) {
	spoken := make([]string, 0, len(names))
	for s := range names {
		spoken = append(spoken, s)
	}
	sort.Strings(spoken)
	entries := make([]branchkit.CollectionPutEntry, 0, len(spoken))
	for _, s := range spoken {
		raw, err := json.Marshal(fileRecord{Spoken: s, Paths: names[s], Cwd: cwd})
		if err != nil {
			return nil, fmt.Errorf("marshal %q: %w", s, err)
		}
		entries = append(entries, branchkit.CollectionPutEntry{ID: s, Payload: raw})
	}
	return entries, nil
}

func reportDigest(cwd string, files []string, changed map[string]bool) string {
	sum := sha256.New()
	fmt.Fprintf(sum, "%s\x00", cwd)
	for _, f := range files {
		mark := "-"
		if changed[f] {
			mark = "+"
		}
		fmt.Fprintf(sum, "%s%s\x00", mark, f)
	}
	return hex.EncodeToString(sum.Sum(nil))
}

// loadProject reads the published names back, so a restarted plugin answers
// "mention" for the last project before the next prompt reports it again. The
// collection is the record of what was published, user-added names included.
func (h *Host) loadProject() error {
	h.mu.Lock()
	before := h.published
	h.mu.Unlock()
	records, err := h.plugin.ListAll(filesCollection)
	if err != nil {
		return fmt.Errorf("read %s: %w", filesCollection, err)
	}
	p := project{names: nameTable{}}
	for _, rec := range records {
		var r fileRecord
		if err := json.Unmarshal(rec.Payload, &r); err != nil || r.Spoken == "" || len(r.Paths) == 0 {
			continue
		}
		p.names[r.Spoken] = r.Paths
		if p.cwd == "" {
			p.cwd = r.Cwd
		}
	}
	h.mu.Lock()
	// A publish that landed during the read installed newer names than the
	// read may have seen; keep those.
	if h.published == before {
		// The collection holds names, not which files changed: keep the
		// changed list the last report gave for this same project, or the
		// "mention" list would lose its head until the next prompt.
		if p.cwd == h.project.cwd {
			p.changed = h.project.changed
		}
		h.project = p
	}
	h.mu.Unlock()
	return nil
}
