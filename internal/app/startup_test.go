package app

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/mikeoertli/github-pr-monitor/internal/core"
)

func TestDefaultStartupRestoresAndDiscovers(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fixture uses a POSIX executable")
	}
	dir := t.TempDir()
	gh, cfg := filepath.Join(dir, "fake gh"), filepath.Join(dir, "gprm_config.toml")
	if err := os.WriteFile(gh, []byte(`#!/bin/sh
case "$*" in
 *graphql*) printf '%s' '{"data":{"repository":{"pullRequest":{"title":"Fixture PR","headRefOid":"head","state":"OPEN","commits":{"nodes":[]}}}}}' ;;
 *search/issues*)
   if [ "$GPRM_STARTUP_TEST_FAIL" = 1 ]; then exit 1; fi
   printf '%s' '{"items":[{"pull_request":{"html_url":"https://github.com/acme/api/pull/1"}},{"pull_request":{"html_url":"https://github.com/acme/api/pull/2"}},{"pull_request":{"html_url":"https://github.com/acme/api/pull/3"}},{"pull_request":{"html_url":"https://github.com/acme/api/pull/1"}}]}' ;;
 *user*) printf '%s' '{"login":"fixture"}' ;;
 *) exit 1 ;;
esac
`), 0700); err != nil {
		t.Fatal(err)
	}
	// Omit startup to exercise the actual default, not a CLI override.
	if err := os.WriteFile(cfg, []byte("auto_quit='never'\n"), 0600); err != nil {
		t.Fatal(err)
	}
	run := func(state string, extra ...string) error {
		t.Helper()
		var out, stderr bytes.Buffer
		args := []string{"--once", "--config", cfg, "--state", state, "--gh", gh}
		return Run(append(args, extra...), &out, &stderr)
	}
	load := func(state string) core.Session {
		t.Helper()
		s, err := core.LoadSessionState(state)
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	t.Run("first launch", func(t *testing.T) {
		state := filepath.Join(t.TempDir(), "session.json")
		if err := run(state); err != nil {
			t.Fatal(err)
		}
		if s := load(state); len(s.PRs) != 3 {
			t.Fatal("first launch did not discover and deduplicate PRs")
		}
	})
	t.Run("restore history and merge discovery", func(t *testing.T) {
		state := filepath.Join(t.TempDir(), "session.json")
		first, _ := core.ParseRef("acme/api#1", "github.com")
		shared, _ := core.ParseRef("acme/shared#4", "github.com")
		dismissed, _ := core.ParseRef("acme/api#3", "github.com")
		start := time.Now().Add(-time.Hour).Truncate(time.Second)
		p := core.NewPR(first, start)
		p.Monitored = 15 * time.Minute
		p.History["test\x00run-1"] = core.Job{Key: "test", RunID: "run-1", Status: "passed", Monitored: 10 * time.Minute}
		if err := core.SaveSession(state, []core.PR{p, core.NewPR(shared, start)}, dismissed); err != nil {
			t.Fatal(err)
		}
		for pass := 0; pass < 2; pass++ {
			if err := run(state); err != nil {
				t.Fatal(err)
			}
			s := load(state)
			if len(s.PRs) != 3 || len(s.Dismissed) != 1 || s.Dismissed[0] != dismissed {
				t.Fatal("restore dropped history, duplicated results, or restored a dismissal")
			}
			seen := map[string]core.PR{}
			for _, pr := range s.PRs {
				seen[pr.Ref.URL] = pr
			}
			if _, ok := seen[shared.URL]; !ok {
				t.Fatal("saved PR absent from discovery was dropped")
			}
			if restored := seen[first.URL]; !restored.FirstSeen.Equal(start) || restored.Monitored < p.Monitored || len(restored.History) != 1 {
				t.Fatal("rediscovery replaced the saved monitoring history")
			}
		}
		before, err := os.ReadFile(state)
		if err != nil {
			t.Fatal(err)
		}
		t.Setenv("GPRM_STARTUP_TEST_FAIL", "1")
		if err := run(state); err == nil {
			t.Fatal("discovery failure was hidden")
		}
		after, err := os.ReadFile(state)
		if err != nil || !bytes.Equal(before, after) {
			t.Fatal("failed startup changed the saved session")
		}
	})
	t.Run("empty override", func(t *testing.T) {
		state := filepath.Join(t.TempDir(), "session.json")
		if err := run(state, "--startup", "empty"); err != nil {
			t.Fatal(err)
		}
		if len(load(state).PRs) != 0 {
			t.Fatal("empty mode unexpectedly discovered PRs")
		}
	})
}
