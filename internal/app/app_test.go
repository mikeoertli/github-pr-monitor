package app

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/mikeoertli/github-pr-monitor/internal/core"
)

func TestDemoDoesNotReadOrWriteUserFiles(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "broken.toml")
	state := filepath.Join(dir, "session.json")
	os.WriteFile(cfg, []byte("not valid toml"), 0600)
	os.WriteFile(state, []byte("leave me alone"), 0600)
	var out, stderr bytes.Buffer
	if err := Run([]string{"--demo", "--once", "--config", cfg, "--state", state}, &out, &stderr); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "GitHub Actions") || !strings.Contains(out.String(), "OBSERVED RUNS") {
		t.Fatal(out.String())
	}
	b, _ := os.ReadFile(state)
	if string(b) != "leave me alone" {
		t.Fatal("demo overwrote session")
	}
}
func TestCLIOverridesAndStartupModes(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fixture uses a POSIX executable")
	}
	dir := t.TempDir()
	cfg := filepath.Join(dir, "config.toml")
	state := filepath.Join(dir, "session.json")
	gh := filepath.Join(dir, "fake gh")
	clip := filepath.Join(dir, "fake clipboard")
	os.WriteFile(gh, []byte(`#!/bin/sh
case "$*" in
 *graphql*) printf '%s' '{"data":{"repository":{"pullRequest":{"title":"Fixture PR","headRefOid":"abc","state":"OPEN","commits":{"nodes":[]}}}}}' ;;
 *search/issues*) printf '%s' '{"items":[{"pull_request":{"html_url":"https://github.com/acme/discovered/pull/2"}}]}' ;;
 *user*) printf '%s' '{"login":"fixture"}' ;;
 *) exit 1 ;;
esac
`), 0700)
	os.WriteFile(clip, []byte("#!/bin/sh\nprintf 'acme/clipboard#3 acme/clipboard#3'\n"), 0700)
	os.WriteFile(cfg, []byte("startup='restore'\nauto_quit='all-closed'\n[tools]\ngh='missing-tool'\nclipboard='"+clip+"'\n"), 0600)
	var out, stderr bytes.Buffer
	run := func(mode string, args ...string) {
		t.Helper()
		out.Reset()
		err := Run(append([]string{"--once", "--config", cfg, "--state", state, "--gh", gh, "--startup", mode, "--auto-quit", "never"}, args...), &out, &stderr)
		if err != nil {
			t.Fatal(err)
		}
	}
	run("empty", "acme/explicit#1")
	prs, err := core.LoadSession(state)
	if err != nil || len(prs) != 1 || prs[0].Ref.Repo != "acme/explicit" {
		t.Fatalf("%+v %v", prs, err)
	}
	run("restore")
	if !strings.Contains(out.String(), "acme/explicit") {
		t.Fatal("restore failed")
	}
	run("clipboard")
	prs, _ = core.LoadSession(state)
	if len(prs) != 1 || prs[0].Ref.Repo != "acme/clipboard" {
		t.Fatal(prs)
	}
	run("auto-discover")
	prs, _ = core.LoadSession(state)
	if len(prs) != 1 || prs[0].Ref.Repo != "acme/discovered" {
		t.Fatal(prs)
	}
	run("empty")
	prs, _ = core.LoadSession(state)
	if len(prs) != 0 {
		t.Fatal(prs)
	}
}
