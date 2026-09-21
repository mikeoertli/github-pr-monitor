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

func TestCLIFilterScopesRequestsAndOutputButNotDiscovery(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fixture uses a POSIX executable")
	}
	dir := t.TempDir()
	gh, cfg, state, log := filepath.Join(dir, "gh"), filepath.Join(dir, "config.toml"), filepath.Join(dir, "session.json"), filepath.Join(dir, "calls")
	t.Setenv("GPRM_FILTER_TEST_LOG", log)
	script := `#!/bin/sh
printf '%s\n' "$*" | tr '\n' ' ' >> "$GPRM_FILTER_TEST_LOG"
printf '\n' >> "$GPRM_FILTER_TEST_LOG"
case "$*" in
 *graphql*) printf '%s' '{"data":{"repository":{"pullRequest":{"title":"Fixture PR","headRefOid":"abc","state":"OPEN","commits":{"nodes":[]}}}}}' ;;
 *search/issues*) printf '%s' '{"items":[{"pull_request":{"html_url":"https://github.com/acme/api/pull/1"}},{"pull_request":{"html_url":"https://github.com/acme/web/pull/2"}}]}' ;;
 *user*) printf '%s' '{"login":"fixture"}' ;;
 *) exit 1 ;;
esac
`
	if err := os.WriteFile(gh, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfg, []byte("auto_quit='never'\n"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name      string
		args      []string
		wantFetch bool
	}{
		{"long", []string{"--startup", "empty", "--filter", "AP #1", "acme/api#1", "acme/web#2"}, true},
		{"short after refs", []string{"--startup", "empty", "acme/api#1", "acme/web#2", "-f", "AP #1"}, true},
		{"restore", []string{"--startup", "restore", "-f", "api"}, true},
		{"discovery", []string{"--startup", "auto-discover", "--filter=api"}, true},
		{"no matches", []string{"--startup", "restore", "-f", "absent"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := os.WriteFile(log, nil, 0600); err != nil {
				t.Fatal(err)
			}
			var out, stderr bytes.Buffer
			args := append([]string{"--once", "--no-color", "--config", cfg, "--state", state, "--gh", gh}, tc.args...)
			if err := Run(args, &out, &stderr); err != nil {
				t.Fatal(err)
			}
			calls, err := os.ReadFile(log)
			if err != nil {
				t.Fatal(err)
			}
			fetches := 0
			for _, line := range strings.Split(string(calls), "\n") {
				if !strings.Contains(line, "graphql") {
					continue
				}
				fetches++
				if !strings.Contains(line, "repo=api") || strings.Contains(line, "repo=web") {
					t.Fatal("polled filtered-out PR")
				}
			}
			want := 0
			if tc.wantFetch {
				want = 1
			}
			if fetches != want {
				t.Fatalf("got %d fetches, want %d", fetches, want)
			}
			if strings.Contains(out.String(), "acme/web") {
				t.Fatal("excluded PR leaked into table or summary")
			}
			if tc.wantFetch && !strings.Contains(out.String(), "acme/api") {
				t.Fatal("matching PR missing")
			}
			if !tc.wantFetch && !strings.Contains(out.String(), "No matches") {
				t.Fatal("missing empty-state explanation")
			}
			prs, err := core.LoadSession(state)
			if err != nil || len(prs) != 2 {
				t.Fatalf("filter dropped saved/discovered PRs: %v %v", prs, err)
			}
			if tc.name == "discovery" && !strings.Contains(string(calls), "search/issues") {
				t.Fatal("filter bypassed discovery")
			}
		})
	}
}

func TestFilterDemoAndHelp(t *testing.T) {
	var out, stderr bytes.Buffer
	if err := Run([]string{"--demo", "--once", "--filter", "plat"}, &out, &stderr); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "acme/platform") || strings.Contains(out.String(), "acme/web") {
		t.Fatal("demo ignored filter")
	}
	if err := Run([]string{"--help"}, &out, &stderr); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stderr.String(), "-f, --filter") {
		t.Fatal("filter missing from help")
	}
}
