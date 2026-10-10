package main

import (
	"sync"
	"time"

	"github.com/branchkit/plugin-sdk-go"
)

// pluginID is this plugin's id, for log lines.
const pluginID = "address"

// platform is every call this plugin makes on the platform, so tests can
// stand in for it. *branchkit.Plugin satisfies it.
type platform interface {
	ListAll(name string) ([]branchkit.CollectionRecord, error)
	Replace(name string, entries []branchkit.CollectionPutEntry, scope branchkit.ReplaceScope, opts ...branchkit.ReplaceOption) (branchkit.ReplaceResult, error)
	Put(name, id string, payload any) error
	Delete(name, id string) (bool, error)
	InputTypeText(branchkit.InputTypeTextRequest) error
	OutputState(branchkit.OutputStateRequest) (*branchkit.OutputStateResponse, error)
	OutputClear(branchkit.OutputClearRequest) (*branchkit.OutputClearResponse, error)
	SettingsRefresh() error
	NativeFormatDate(branchkit.NativeFormatDateRequest) (*branchkit.NativeFormatDateResponse, error)
}

// Host is what every handler needs. Handlers are methods on it, so a
// handler's dependencies are visible in its signature.
type Host struct {
	plugin platform
	lex    lexicon

	mu sync.Mutex
	// project is the project of the latest report: its folder and the names
	// of its files.
	project project
	// learned maps learnKey(cwd, spoken) to the file the person picked for
	// that name in that project.
	learned map[string]string
	// saveMu runs saveLately one at a time.
	saveMu sync.Mutex
	// choosing is the ambiguous name being chosen between, or nil.
	choosing *choice
	// recentBy is, per project folder, the files most recently typed there,
	// newest first: the rest of the list "mention" alone shows. Kept in the
	// lately collection, which is the truth (re-read when the list opens).
	recentBy map[string][]string
	// published counts the projects publish has installed, so a read-back
	// that started before one does not overwrite it with older names.
	published int
	// heard is when the hook last reported a project, zero until it has
	// since this plugin started: the Claude Code tab's evidence that the
	// hook is set up.
	heard time.Time

	// Reports are published one at a time, the newest winning: a report
	// that arrives while another publishes replaces any still waiting.
	queueMu    sync.Mutex
	queued     *projectReport
	publishing bool
	lastDigest string
}

func newHost(p platform, lex lexicon) *Host {
	return &Host{plugin: p, lex: lex, learned: map[string]string{}, recentBy: map[string][]string{}}
}
