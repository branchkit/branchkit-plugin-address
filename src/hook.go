package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/branchkit/plugin-sdk-go"
)

// The Claude Code hook: `address-plugin claude-hook`, run by Claude Code on
// SessionStart and UserPromptSubmit. It is this same binary run as a
// command, not as the plugin, so it works wherever the plugin does with
// nothing else installed.
//
// It reads the hook's input (the session's folder), lists that folder's
// files with git, and sends them to the running plugin through the socket
// the plugin listens on. It never fails the prompt: every problem is
// reported on stderr and the exit status is 0, and it prints nothing on
// stdout, which Claude Code would add to the prompt.

// hookTimeout bounds the whole hook. Claude Code waits for it before
// sending the prompt.
const hookTimeout = 3 * time.Second

// hookInput is the part of Claude Code's hook input this reads.
type hookInput struct {
	Cwd           string `json:"cwd"`
	SessionID     string `json:"session_id"`
	HookEventName string `json:"hook_event_name"`
}

func runClaudeHook(stdin io.Reader, stderr io.Writer) int {
	ctx, cancel := context.WithTimeout(context.Background(), hookTimeout)
	defer cancel()
	if err := claudeHook(ctx, stdin); err != nil {
		fmt.Fprintf(stderr, "address: %v\n", err)
	}
	return 0
}

func claudeHook(ctx context.Context, stdin io.Reader) error {
	var in hookInput
	if err := json.NewDecoder(io.LimitReader(stdin, 1<<20)).Decode(&in); err != nil {
		return fmt.Errorf("read hook input: %w", err)
	}
	if in.Cwd == "" {
		return fmt.Errorf("hook input has no cwd")
	}
	rep, err := listProject(ctx, in.Cwd)
	if err != nil {
		return err
	}
	if rep == nil {
		return nil // not a git project: nothing to name
	}
	rep.SessionID = in.SessionID
	rep.Event = in.HookEventName

	info, err := findPlugin()
	if err != nil {
		return err
	}
	return sendReport(ctx, info, rep)
}

// listProject lists the files under cwd, relative to it (which is how a
// Claude Code mention resolves), with the ones the working tree changed or
// added marked. nil when cwd is not inside a git work tree.
func listProject(ctx context.Context, cwd string) (*projectReport, error) {
	git := func(args ...string) ([]string, error) {
		cmd := exec.CommandContext(ctx, "git", append([]string{"-C", cwd}, args...)...)
		var out bytes.Buffer
		cmd.Stdout = &out
		if err := cmd.Run(); err != nil {
			return nil, err
		}
		var paths []string
		for _, p := range strings.Split(out.String(), "\x00") {
			if p != "" {
				paths = append(paths, p)
			}
		}
		return paths, nil
	}
	if _, err := git("rev-parse", "--is-inside-work-tree"); err != nil {
		return nil, nil
	}
	tracked, err := git("ls-files", "-z", "--recurse-submodules")
	if err != nil {
		// --recurse-submodules refuses some layouts (a sparse checkout);
		// the project's own files are still worth naming.
		if tracked, err = git("ls-files", "-z"); err != nil {
			return nil, fmt.Errorf("git ls-files in %s: %w", cwd, err)
		}
	}
	added, _ := git("ls-files", "-z", "--others", "--exclude-standard")
	modified, _ := git("ls-files", "-z", "--modified")
	return &projectReport{
		Cwd:     cwd,
		Files:   append(tracked, added...),
		Changed: append(modified, added...),
	}, nil
}

// findPlugin reads the connect.json the running plugin wrote beside its
// binary, which is this binary.
func findPlugin() (branchkit.ConnectInfo, error) {
	var dirs []string
	if exe, err := os.Executable(); err == nil {
		if resolved, err := filepath.EvalSymlinks(exe); err == nil {
			exe = resolved
		}
		dirs = append(dirs, filepath.Dir(exe))
	}
	if d := os.Getenv("BRANCHKIT_PLUGIN_DIR"); d != "" {
		dirs = append(dirs, d)
	}
	for _, d := range dirs {
		raw, err := os.ReadFile(filepath.Join(d, "connect.json"))
		if err != nil {
			continue
		}
		var info branchkit.ConnectInfo
		if err := json.Unmarshal(raw, &info); err != nil || info.Port == "" || info.Token == "" {
			return info, fmt.Errorf("%s/connect.json is unreadable", d)
		}
		return info, nil
	}
	return branchkit.ConnectInfo{}, fmt.Errorf("the address plugin is not running (no connect.json beside it)")
}

func sendReport(ctx context.Context, info branchkit.ConnectInfo, rep *projectReport) error {
	body, err := json.Marshal(rep)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://127.0.0.1:"+info.Port+"/project", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+info.Token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("send to the address plugin: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("the address plugin refused the report: %s %s", resp.Status, strings.TrimSpace(string(msg)))
	}
	return nil
}
