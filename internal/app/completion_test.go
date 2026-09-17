package app

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestCompletionGenerationIsOfflineAndSupportsAliases(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "config"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(dir, "state"))
	for _, shell := range []string{"bash", "zsh", "fish", "powershell"} {
		t.Run(shell, func(t *testing.T) {
			var out, stderr bytes.Buffer
			if err := Run([]string{"completion", shell}, &out, &stderr); err != nil {
				t.Fatal(err)
			}
			for _, name := range []string{"gprm", "ghprm", "github-pr-monitor"} {
				if !strings.Contains(out.String(), name) {
					t.Fatalf("missing %s registration", name)
				}
			}
			if stderr.Len() != 0 {
				t.Fatal(stderr.String())
			}
			if shell == "bash" || shell == "zsh" {
				path, err := exec.LookPath(shell)
				if err == nil {
					cmd := exec.Command(path, "-n")
					cmd.Stdin = strings.NewReader(out.String())
					if b, err := cmd.CombinedOutput(); err != nil {
						t.Fatalf("shell syntax: %v: %s", err, b)
					}
				}
			}
		})
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 0 {
		t.Fatalf("completion wrote user files: %v %v", entries, err)
	}
}

func TestCompletionValuesAndFlags(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want []string
	}{
		{[]string{"--startup", ""}, []string{"restore", "clipboard", "empty", "auto-discover", ":4"}},
		{[]string{"--auto-quit", ""}, []string{"never", "builds-finished", "all-passing", "all-closed", ":4"}},
		{[]string{"-m", ""}, []string{"restore", "clipboard", "empty", "auto-discover", ":4"}},
		{[]string{"--sort", ""}, []string{"repo", "progress", ":4"}},
		{[]string{"--interval", ""}, []string{"5s", "10s", "1m"}},
		{[]string{"--completed-retention", ""}, []string{"24h", "forever", "0s"}},
		{[]string{"completion", ""}, []string{"bash", "zsh", "fish", "powershell"}},
		{[]string{"--"}, []string{"--config", "--state", "--startup", "--auto-quit", "--interval", "--sort", "--gh", "--demo", "--once", "--init-config", "--version", "--no-color"}},
	} {
		var out, stderr bytes.Buffer
		if err := Run(append([]string{"__complete"}, tc.args...), &out, &stderr); err != nil {
			t.Fatal(err)
		}
		for _, want := range tc.want {
			if !strings.Contains(out.String(), want) {
				t.Fatalf("%v missing %q in %q", tc.args, want, out.String())
			}
		}
	}
	// A completion request with invalid live settings still never starts the app.
	var out, stderr bytes.Buffer
	if err := Run([]string{"__complete", "--config", "/missing/config", "--gh", "/missing/gh", "--startup", ""}, &out, &stderr); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "auto-discover") {
		t.Fatal(out.String())
	}
}

func TestCompletionErrors(t *testing.T) {
	for _, args := range [][]string{{"completion"}, {"completion", "unknown"}, {"completion", "bash", "extra"}} {
		var out, stderr bytes.Buffer
		if err := Run(args, &out, &stderr); err == nil {
			t.Fatalf("accepted %v", args)
		}
	}
}
