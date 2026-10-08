package main

import (
	"bytes"
	"encoding/json"
	"html/template"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/branchkit/plugin-sdk-go"
)

// The Claude Code tab: whether the hook has reported a project, and how to
// add it. The hook is the only way this plugin learns a project's files, and
// adding it means editing Claude Code's settings by hand, so the steps live
// where a person looks when "mention" hears nothing: closed once it works, open
// until it has.

// hookStatus is how far the hook has got.
type hookStatus int

const (
	// hookNever: no project has been reported, now or before a restart.
	hookNever hookStatus = iota
	// hookEarlier: names from a report before this run, none since.
	hookEarlier
	// hookConnected: a report has arrived since the plugin started.
	hookConnected
)

type claudeTabData struct {
	Status  hookStatus
	Cwd     string
	Heard   string
	Names   int
	Files   int
	Counts  string
	Snippet string
	Lines   int
}

func (h *Host) renderClaudeTab(_ *branchkit.RenderSettingsRequest) (string, error) {
	h.mu.Lock()
	data := claudeTabData{Cwd: h.project.cwd, Names: len(h.project.names)}
	files := map[string]bool{}
	for _, paths := range h.project.names {
		for _, p := range paths {
			files[p] = true
		}
	}
	data.Files = len(files)
	data.Counts = plural(data.Names, "name") + " for " + plural(data.Files, "file")
	heard := h.heard
	h.mu.Unlock()

	switch {
	case !heard.IsZero():
		data.Status = hookConnected
		data.Heard = heardAt(heard, time.Now())
	case data.Names > 0:
		data.Status = hookEarlier
	}
	snippet, err := hookSettings(hookCommand(pluginBinary()))
	if err != nil {
		return "", err
	}
	data.Snippet = snippet
	data.Lines = strings.Count(snippet, "\n") + 1

	var buf bytes.Buffer
	if err := claudeTab.Execute(&buf, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}

func plural(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return strconv.Itoa(n) + " " + noun + "s"
}

// heardAt says when a report arrived: the time alone today, the date too
// before that.
func heardAt(t, now time.Time) string {
	t, now = t.Local(), now.Local()
	if y, m, d := t.Date(); y == now.Year() && m == now.Month() && d == now.Day() {
		return t.Format("3:04 PM")
	}
	return t.Format("Jan 2, 3:04 PM")
}

// pluginBinary is the path of this plugin's binary as the person's
// installation names it: the plugin folder BranchKit gave it, which is the
// path that stays valid across updates, and the binary's own name.
func pluginBinary() string {
	exe, err := os.Executable()
	if err != nil {
		exe = "address-plugin"
	}
	if dir := os.Getenv("BRANCHKIT_PLUGIN_DIR"); dir != "" {
		return filepath.Join(dir, filepath.Base(exe))
	}
	return exe
}

// hookCommand is the shell command Claude Code runs: the binary, quoted for
// a POSIX shell (the plugin folder is under "Application Support"), run as
// the hook.
func hookCommand(binary string) string {
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`, `$`, `\$`, "`", "\\`")
	return `"` + r.Replace(binary) + `" claude-hook`
}

// hookSettings is the "hooks" block to add to ~/.claude/settings.json, the
// same hook on both events (hook.go says why both).
func hookSettings(command string) (string, error) {
	type hook struct {
		Type    string `json:"type"`
		Command string `json:"command"`
		Timeout int    `json:"timeout"`
	}
	type matcher struct {
		Hooks []hook `json:"hooks"`
	}
	entry := []matcher{{Hooks: []hook{{Type: "command", Command: command, Timeout: 5}}}}
	block := struct {
		Hooks struct {
			SessionStart     []matcher `json:"SessionStart"`
			UserPromptSubmit []matcher `json:"UserPromptSubmit"`
		} `json:"hooks"`
	}{}
	block.Hooks.SessionStart = entry
	block.Hooks.UserPromptSubmit = entry
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(block); err != nil {
		return "", err
	}
	return strings.TrimSpace(buf.String()), nil
}

var claudeTab = template.Must(template.New("claude").Parse(`
<div class="page-head">
	<h2 class="page-title">Claude Code</h2>
	<p class="page-lede">"Mention" names the files of the project you are working on in Claude Code. Claude Code tells BranchKit which project that is through a hook, added once to its settings.</p>
</div>
<bk-table columns="140px 1fr">
	<div class="settings-row">
		<div class="label">Status</div>
		<div class="value">
			{{- if eq .Status 2}}<span class="badge badge-running">Connected</span> Claude Code reported this project at {{.Heard}}.
			{{- else if eq .Status 1}}<span class="badge badge-meta">Waiting</span> These names are from an earlier session. They refresh the next time you send a prompt in Claude Code.
			{{- else}}<span class="badge badge-restart">Not set up</span> Claude Code has not reported a project yet, so "mention" has no files to name.
			{{- end}}
		</div>
	</div>
	{{- if .Cwd}}
	<div class="settings-row">
		<div class="label">Project</div>
		<div class="value mono">{{.Cwd}}</div>
	</div>
	<div class="settings-row">
		<div class="label">Names</div>
		<div class="value">{{.Counts}}</div>
	</div>
	{{- end}}
</bk-table>
<details class="page-more address-setup"{{if eq .Status 0}} open{{end}}>
	<summary>How to set up the hook</summary>
	<div>
		<ol>
			<li>Open <code>~/.claude/settings.json</code>, Claude Code's settings in your home folder. Create the file if it is not there.</li>
			<li>Add the hooks below. If the file already has a <code>"hooks"</code> section, add these two entries to it rather than replacing it.</li>
			<li>Send a prompt in Claude Code. This page shows the project once it arrives.</li>
		</ol>
		<textarea class="mono address-snippet" readonly rows="{{.Lines}}" spellcheck="false"
			aria-label="Hooks to add to ~/.claude/settings.json"
			data-on:focus="evt.target.select()">{{.Snippet}}</textarea>
		<p>The hook runs this plugin's own program at the start of each session and before each prompt. It lists the project's files with git and sends them here. It never changes or delays a prompt.</p>
	</div>
</details>
`))
