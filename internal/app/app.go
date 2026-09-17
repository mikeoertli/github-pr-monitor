// Package app provides the CLI entry point and platform integrations.
package app

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"strings"
	"syscall"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	project "github.com/mikeoertli/github-pr-monitor"
	"github.com/mikeoertli/github-pr-monitor/internal/config"
	"github.com/mikeoertli/github-pr-monitor/internal/core"
	"github.com/mikeoertli/github-pr-monitor/internal/provider"
	"github.com/mikeoertli/github-pr-monitor/internal/tui"
	"github.com/spf13/pflag"
)

func Run(args []string, out, stderr io.Writer) error {
	flags := pflag.NewFlagSet("gprm", pflag.ContinueOnError)
	flags.SetOutput(stderr)
	cfgPath := flags.StringP("config", "c", config.Path(), "TOML settings file")
	statePath := flags.String("state", config.StatePath(), "saved monitoring session")
	startup := flags.StringP("startup", "m", "", "restore | clipboard | empty | auto-discover")
	quit := flags.StringP("auto-quit", "q", "", "never | builds-finished | all-passing | all-closed")
	retention := flags.String("completed-retention", "", "keep merged/closed PRs for a duration (24h), forever, or 0s")
	interval := flags.StringP("interval", "i", "", "status refresh interval, e.g. 5s")
	sortBy := flags.StringP("sort", "s", "", "repo | progress")
	gh := flags.String("gh", "", "gh executable path")
	demo := flags.Bool("demo", false, "offline demo (does not read or write your session)")
	once := flags.Bool("once", false, "fetch once, print a snapshot and summary, then exit")
	initConfig := flags.Bool("init-config", false, "create a commented config file without overwriting an existing one")
	version := flags.BoolP("version", "V", false, "print version")
	noColor := flags.Bool("no-color", false, "disable color and text styling")
	help := flags.BoolP("help", "h", false, "show help")
	flags.Usage = func() {
		fmt.Fprint(stderr, "Usage: gprm [flags] [PR-URL | owner/repo#123 ...]\n       gprm completion bash|zsh|fish|powershell\n\nGitHub PR and build tracker. Flags may appear before or after PR references.\nDefault: restore previous session; quit when all monitored PRs are merged/closed.\n\n")
		flags.PrintDefaults()
	}
	// Completion runs before configuration, credentials, or the TUI are touched.
	if len(args) > 0 && (args[0] == "completion" || args[0] == "__complete" || args[0] == "__completeNoDesc") {
		return runCompletion(flags, args, out, stderr)
	}
	if err := flags.Parse(args); err != nil {
		if err == pflag.ErrHelp {
			return nil
		}
		return err
	}
	if *help {
		flags.Usage()
		return nil
	}
	if *version {
		fmt.Fprintln(out, "gprm "+project.Version())
		return nil
	}
	if *initConfig {
		if err := config.WriteExample(*cfgPath); err != nil {
			return err
		}
		fmt.Fprintln(out, "Created "+*cfgPath)
		return nil
	}
	c := config.Defaults()
	var err error
	if !*demo {
		c, err = config.Load(*cfgPath)
		if err != nil {
			return err
		}
	}
	if *startup != "" {
		c.Startup = *startup
	}
	if *quit != "" {
		c.AutoQuit = *quit
	}
	if *retention != "" {
		c.CompletedRetention = *retention
	}
	if *interval != "" {
		c.Interval = *interval
	}
	if *sortBy != "" {
		c.Sort = *sortBy
	}
	if *gh != "" {
		c.Tools.GH = *gh
	}
	if flags.Changed("no-color") {
		c.NoColor = *noColor
	}
	if *demo && *quit == "" {
		c.AutoQuit = "never"
	}
	if err = c.Validate(); err != nil {
		return err
	}
	if !*demo {
		if _, err = exec.LookPath(c.Tools.GH); err != nil {
			return fmt.Errorf("gh is required: install GitHub CLI, run gh auth login, or set tools.gh / --gh to its path")
		}
		if _, err = os.Stat(*cfgPath); os.IsNotExist(err) {
			if err = config.WriteExample(*cfgPath); err != nil {
				return fmt.Errorf("create config: %w", err)
			}
		}
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	source := provider.New(c)
	actions := tui.Actions{Copy: func(ctx context.Context, text string) error { return CopyClipboard(ctx, c, text) }, Clipboard: func(ctx context.Context) (string, error) { return Clipboard(ctx, c) }, Open: func(ctx context.Context, raw string) error { return Open(ctx, c, raw) }}
	var prs []core.PR
	var saved core.Session
	if *demo {
		prs = tui.DemoPRs()
	} else {
		var refs []core.Ref
		switch c.Startup {
		case "restore":
			saved, err = core.LoadSessionState(*statePath)
			prs = saved.PRs
		case "clipboard":
			var text string
			text, err = Clipboard(ctx, c)
			if err == nil {
				refs, err = core.ParseBatch(text, c.GitHubHost)
			}
		case "auto-discover":
			saved, err = core.LoadSessionState(*statePath)
			if err == nil {
				refs, err = source.Discover(ctx)
			}
			for _, p := range saved.PRs {
				if p.Closed() {
					prs = append(prs, p)
				}
			}
			if err == nil && len(refs) >= c.DiscoveryLimit {
				fmt.Fprintln(stderr, "Discovery limit reached; increase discovery_limit to include more PRs.")
			}
		}
		if err != nil {
			return err
		}
		for _, ref := range refs {
			skip := false
			if c.Startup == "auto-discover" {
				for _, d := range saved.Dismissed {
					if strings.EqualFold(d.URL, ref.URL) {
						skip = true
					}
				}
			}
			for _, p := range prs {
				if strings.EqualFold(p.Ref.URL, ref.URL) {
					skip = true
				}
			}
			if !skip {
				prs = append(prs, core.NewPR(ref, time.Now()))
			}
		}
	}
	for _, arg := range flags.Args() {
		ref, e := core.ParseRef(arg, c.GitHubHost)
		if e != nil {
			return e
		}
		exists := false
		for _, p := range prs {
			if strings.EqualFold(p.Ref.URL, ref.URL) {
				exists = true
			}
		}
		if !exists {
			prs = append(prs, core.NewPR(ref, time.Now()))
		}
	}
	m := tui.New(ctx, c, prs, source, actions, *statePath, *demo)
	for _, ref := range saved.Dismissed {
		m.Dismissed[strings.ToLower(ref.URL)] = ref
	}
	for _, p := range prs {
		delete(m.Dismissed, strings.ToLower(p.Ref.URL))
	}
	if *once {
		start := time.Now()
		if *demo {
			refs := []core.Ref{}
			for _, p := range prs {
				refs = append(refs, p.Ref)
			}
			for i, p := range tui.DemoSnapshots(refs, 1) {
				m.PRs[i].Apply(p, time.Now())
			}
		} else {
			for i, p := range prs {
				m.PRs[i].Apply(source.Fetch(ctx, p.Ref), time.Now())
			}
		}
		m.ExpireCompleted(time.Now())
		m.Finish()
		fmt.Fprintln(out, m.View())
		fmt.Fprintln(out)
		fmt.Fprint(out, core.Summary(m.PRs, time.Since(start)))
		if m.SaveError != nil {
			return fmt.Errorf("save session: %w", m.SaveError)
		}
		for _, p := range m.PRs {
			if p.Error != "" {
				return fmt.Errorf("one or more PRs could not be refreshed")
			}
		}
		return nil
	}
	// Handle signals as a normal quit so the terminal restores and summary prints.
	program := tea.NewProgram(m, tea.WithAltScreen(), tea.WithOutput(out), tea.WithoutSignalHandler())
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			program.Quit()
		case <-done:
		}
	}()
	_, err = program.Run()
	m.Finish()
	fmt.Fprintln(out, core.Summary(m.PRs, time.Since(m.Started)))
	if m.QuitReason != "" {
		fmt.Fprintln(out, m.QuitReason)
	}
	if err == nil && m.SaveError != nil {
		return fmt.Errorf("save session: %w", m.SaveError)
	}
	return err
}

func Clipboard(ctx context.Context, c config.Config) (string, error) {
	path, args := c.Tools.Clipboard, append([]string(nil), c.Tools.ClipboardArgs...)
	if path == "" {
		switch runtime.GOOS {
		case "darwin":
			path = "/usr/bin/pbpaste"
		case "windows":
			path = "powershell.exe"
			args = []string{"-NoProfile", "-NonInteractive", "-Command", "Get-Clipboard -Raw"}
		default:
			if os.Getenv("WAYLAND_DISPLAY") != "" {
				path = "wl-paste"
				args = []string{"--no-newline"}
			} else if _, err := exec.LookPath("xclip"); err == nil {
				path = "xclip"
				args = []string{"-selection", "clipboard", "-o"}
			} else {
				path = "xsel"
				args = []string{"--clipboard", "--output"}
			}
		}
	}
	ctx, cancel := context.WithTimeout(ctx, c.RequestTimeout())
	defer cancel()
	b, err := exec.CommandContext(ctx, path, args...).Output()
	if err != nil {
		return "", fmt.Errorf("could not read clipboard; use a to paste manually or configure tools.clipboard")
	}
	if len(b) > 1<<20 {
		return "", fmt.Errorf("clipboard exceeds 1 MiB; copy just the PR links")
	}
	return string(b), nil
}

// CopyClipboard sends JSON via stdin, keeping its contents out of command arguments.
func CopyClipboard(ctx context.Context, c config.Config, text string) error {
	path, args := c.Tools.ClipboardWrite, append([]string(nil), c.Tools.ClipboardWriteArgs...)
	if path == "" {
		switch runtime.GOOS {
		case "darwin":
			path = "/usr/bin/pbcopy"
		case "windows":
			path = "powershell.exe"
			args = []string{"-NoProfile", "-Command", "[Console]::InputEncoding = [System.Text.Encoding]::UTF8; Set-Clipboard -Value ([Console]::In.ReadToEnd())"}
		default:
			if _, err := exec.LookPath("wl-copy"); err == nil {
				path = "wl-copy"
			} else if _, err := exec.LookPath("xclip"); err == nil {
				path, args = "xclip", []string{"-selection", "clipboard", "-in"}
			} else {
				path, args = "xsel", []string{"--clipboard", "--input"}
			}
		}
	}
	ctx, cancel := context.WithTimeout(ctx, c.RequestTimeout())
	defer cancel()
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Stdin = strings.NewReader(text)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("could not copy JSON; configure tools.clipboard_write")
	}
	return nil
}

func Open(ctx context.Context, c config.Config, raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.User != nil || (u.Scheme != "https" && u.Scheme != "http") {
		return fmt.Errorf("build has no valid HTTP(S) URL")
	}
	path, args := c.Tools.Open, append([]string(nil), c.Tools.OpenArgs...)
	if path == "" {
		switch runtime.GOOS {
		case "darwin":
			path = "/usr/bin/open"
		case "windows":
			path = "rundll32.exe"
			args = []string{"url.dll,FileProtocolHandler"}
		default:
			path = "xdg-open"
		}
	}
	ctx, cancel := context.WithTimeout(ctx, c.RequestTimeout())
	defer cancel()
	if err = exec.CommandContext(ctx, path, append(args, raw)...).Run(); err != nil {
		return fmt.Errorf("could not open link; configure tools.open")
	}
	return nil
}
