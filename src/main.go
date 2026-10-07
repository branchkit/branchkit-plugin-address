package main

import (
	_ "embed"
	"encoding/json"
	"os"

	"github.com/branchkit/plugin-sdk-go"
)

//go:embed settings.css
var settingsCSS string

func main() {
	// The same binary is the Claude Code hook (hook.go).
	if len(os.Args) > 1 && os.Args[1] == "claude-hook" {
		os.Exit(runClaudeHook(os.Stdin, os.Stderr))
	}

	p := branchkit.NewPlugin()
	h := newHost(p, englishLexicon())

	// Registrars come from actions_gen.go, generated from plugin.json, so no
	// action string is spelled here and a params type cannot drift from the
	// manifest.
	HandleCite(p, func(params CiteParams, _ *branchkit.OnActionRequest) (any, error) {
		return nil, h.cite(params.Name)
	})
	HandleInsert(p, func(params InsertParams, _ *branchkit.OnActionRequest) (any, error) {
		return nil, h.insert(params.Path)
	})
	HandleDismiss(p, func(*branchkit.OnActionRequest) (any, error) {
		h.closeChoices()
		return nil, nil
	})

	p.SettingsCSS(settingsCSS)
	p.SettingsTab("claude_code", h.renderClaudeTab)

	// A choice lasts one hold: the gate and the badges clear themselves when
	// it ends (manifest lifecycle); the window and the open choice are this
	// plugin's to close.
	p.On("_platform.input.session_boundary", func(json.RawMessage) { h.closeChoices() })

	// Names published before a restart answer "cite" until the next prompt
	// reports the project again; the warm-up of the word list is paid here,
	// not on the first report.
	p.OnReady(func() {
		englishLexicon()
		if err := h.loadProject(); err != nil {
			branchkit.Logf(pluginID, "%v", err)
		}
		if err := h.loadLearned(); err != nil {
			branchkit.Logf(pluginID, "%v", err)
		}
	})

	// The hook's way in: the socket the manifest asks for (sockets.listen),
	// with its port and pairing token written to connect.json beside this
	// binary, where the hook looks.
	ln, err := branchkit.ListenLocal(p)
	if err != nil {
		branchkit.Logf(pluginID, "listen for the Claude Code hook: %v (names will not update)", err)
	} else {
		ln.HandleFunc("POST /project", h.handleProject)
		go func() {
			if err := ln.Serve(); err != nil {
				branchkit.Logf(pluginID, "hook listener stopped: %v", err)
			}
		}()
	}

	p.Run()
}
