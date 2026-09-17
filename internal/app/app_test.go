package app

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/mikeoertli/github-pr-monitor/internal/config"
	"github.com/mikeoertli/github-pr-monitor/internal/core"
)

func TestClipboardWriterHelper(t *testing.T) {
	if os.Getenv("GPRM_TEST_CLIPBOARD_HELPER") != "1" {
		return
	}
	b, err := io.ReadAll(os.Stdin)
	if err != nil {
		os.Exit(2)
	}
	if os.Getenv("GPRM_TEST_CLIPBOARD_FAIL") == "1" {
		os.Exit(3)
	}
	if err = os.WriteFile(os.Getenv("GPRM_TEST_CLIPBOARD_FILE"), b, 0600); err != nil {
		os.Exit(4)
	}
	os.Exit(0)
}

func TestCopyClipboardUsesStdinAndReportsFailure(t *testing.T) {
	c := config.Defaults()
	c.Tools.ClipboardWrite = os.Args[0]
	c.Tools.ClipboardWriteArgs = []string{"-test.run=^TestClipboardWriterHelper$"}
	path := filepath.Join(t.TempDir(), "copied.json")
	t.Setenv("GPRM_TEST_CLIPBOARD_HELPER", "1")
	t.Setenv("GPRM_TEST_CLIPBOARD_FILE", path)
	want := "{\"title\":\"quotes ' and unicode → $(unchanged)\"}\n"
	if err := CopyClipboard(context.Background(), c, want); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil || string(b) != want {
		t.Fatalf("clipboard changed: %q %v", b, err)
	}
	t.Setenv("GPRM_TEST_CLIPBOARD_FAIL", "1")
	if err := CopyClipboard(context.Background(), c, want); err == nil {
		t.Fatal("copy failure hidden")
	}
}

func TestVersionAndDefaultConfigInitialization(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	want, err := os.ReadFile("../../VERSION")
	if err != nil {
		t.Fatal(err)
	}
	var out, stderr bytes.Buffer
	if err := Run([]string{"--version", "--config", "/missing/config", "--gh", "/missing/gh"}, &out, &stderr); err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(out.String()); got != "gprm "+strings.TrimSpace(string(want)) {
		t.Fatalf("version %q does not match VERSION", got)
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 0 {
		t.Fatalf("version wrote user files: %v %v", entries, err)
	}
	out.Reset()
	if err := Run([]string{"--init-config"}, &out, &stderr); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "gprm", "gprm_config.toml")
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), path) {
		t.Fatal(out.String())
	}
	if err := Run([]string{"--init-config"}, &out, &stderr); err == nil {
		t.Fatal("overwrote existing default config")
	}
}

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
	cfg := filepath.Join(dir, "gprm_config.toml")
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

func TestStandardFlagsAndNoColor(t *testing.T) {
	for _, help := range []string{"-h", "--help"} {
		var out, stderr bytes.Buffer
		if err := Run([]string{help}, &out, &stderr); err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{"-h, --help", "-i, --interval", "--no-color"} {
			if !strings.Contains(stderr.String(), want) {
				t.Fatalf("help lacks %s: %s", want, stderr.String())
			}
		}
	}
	for _, args := range [][]string{{"--demo", "--once", "--no-color"}, {"--demo", "acme/test#1", "--once", "--no-color", "-i", "10s", "-s", "progress", "-m", "empty", "-q", "never"}} {
		var out, stderr bytes.Buffer
		if err := Run(args, &out, &stderr); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(out.String(), "\x1b") {
			t.Fatal("--no-color emitted styling")
		}
	}
	for _, arg := range []string{"-help", "-version", "-interval"} {
		var out, stderr bytes.Buffer
		if err := Run([]string{arg}, &out, &stderr); err == nil {
			t.Fatalf("accepted nonstandard long flag %s", arg)
		}
	}
	var out, stderr bytes.Buffer
	if err := Run([]string{"-V"}, &out, &stderr); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(out.String(), "gprm ") {
		t.Fatal(out.String())
	}
}

func TestAutoDiscoverRetainsCompletedRowsAndDismissals(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fixture uses POSIX executable")
	}
	dir := t.TempDir()
	cfg := filepath.Join(dir, "gprm_config.toml")
	state := filepath.Join(dir, "session.json")
	gh := filepath.Join(dir, "fake gh")
	if err := os.WriteFile(gh, []byte(`#!/bin/sh
case "$*" in
 *graphql*number=1*) printf '%s' '{"data":{"repository":{"pullRequest":{"title":"Completed","state":"MERGED","headRefOid":"a","merged":true,"mergedAt":"2020-01-01T00:00:00Z","commits":{"nodes":[]}}}}}' ;;
 *graphql*) printf '%s' '{"data":{"repository":{"pullRequest":{"title":"Open","state":"OPEN","headRefOid":"b","commits":{"nodes":[]}}}}}' ;;
 *search/issues*) printf '%s' '{"items":[{"pull_request":{"html_url":"https://github.com/acme/api/pull/2"}}]}' ;;
 *user*) printf '%s' '{"login":"fixture"}' ;;
 *) exit 1 ;;
esac
`), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfg, []byte("completed_retention='forever'\n"), 0600); err != nil {
		t.Fatal(err)
	}
	a, _ := core.ParseRef("acme/api#1", "github.com")
	b, _ := core.ParseRef("acme/api#2", "github.com")
	prs := []core.PR{core.NewPR(a, time.Now()), core.NewPR(b, time.Now())}
	prs[0].State = "MERGED"
	prs[1].Removed = true
	if err := core.SaveSession(state, prs); err != nil {
		t.Fatal(err)
	}
	run := func(extra ...string) core.Session {
		t.Helper()
		var out, stderr bytes.Buffer
		args := []string{"--once", "--startup", "auto-discover", "--auto-quit", "never", "--config", cfg, "--state", state, "--gh", gh}
		if err := Run(append(args, extra...), &out, &stderr); err != nil {
			t.Fatal(err)
		}
		s, err := core.LoadSessionState(state)
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	s := run()
	if len(s.PRs) != 1 || s.PRs[0].Ref.Number != 1 || len(s.Dismissed) != 1 {
		t.Fatal("auto-discover lost completed row or dismissal")
	}
	s = run("acme/api#2")
	if len(s.PRs) != 2 || len(s.Dismissed) != 0 {
		t.Fatal("explicit CLI add did not override dismissal")
	}
	s = run("--completed-retention", "0s")
	if len(s.PRs) != 1 || s.PRs[0].Ref.Number != 2 {
		t.Fatal("retention CLI override ignored")
	}
}
