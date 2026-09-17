package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParseBatch(t *testing.T) {
	refs, err := ParseBatch("[first](https://github.com/acme/api/pull/42/files)\nacme/web#3, https://github.com/acme/api/pull/42?x=1", "github.com")
	if err != nil || len(refs) != 2 || refs[0].URL != "https://github.com/acme/api/pull/42" {
		t.Fatalf("%+v, %v", refs, err)
	}
	for _, raw := range []string{"http://github.com/acme/api/pull/1", "https://user:pass@github.com/acme/api/pull/1", "https://github.com/acme/api/issues/1", "acme/api#0", "--help"} {
		if _, err := ParseRef(raw, "github.com"); err == nil {
			t.Errorf("accepted %s", raw)
		}
	}
	if _, err := ParseBatch("nothing here", "github.com"); err == nil {
		t.Fatal("accepted empty batch")
	}
}
func TestQuitMatrix(t *testing.T) {
	for _, tc := range []struct {
		name, mode, state, status string
		fresh, stale, want        bool
	}{
		{"default open", "all-closed", "OPEN", "passed", true, false, false},
		{"merged failure", "all-closed", "MERGED", "failed", true, false, true},
		{"closed pending", "all-closed", "CLOSED", "queued", true, false, true},
		{"closed CI unreachable", "all-closed", "CLOSED", "queued", true, true, true},
		{"closed PR stale", "all-closed", "CLOSED", "passed", false, false, false},
		{"failed done", "builds-finished", "OPEN", "failed", true, false, true},
		{"failed not passing", "all-passing", "OPEN", "failed", true, false, false},
		{"queued", "builds-finished", "OPEN", "queued", true, false, false},
		{"unknown", "all-passing", "OPEN", "unknown", true, false, false},
		{"passed", "all-passing", "OPEN", "passed", true, false, true},
		{"neutral", "all-passing", "OPEN", "neutral", true, false, true},
		{"skipped", "all-passing", "OPEN", "skipped", true, false, true},
		{"stale Jenkins", "builds-finished", "OPEN", "passed", true, true, false},
		{"never", "never", "CLOSED", "passed", true, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := PR{State: tc.state, Fresh: tc.fresh, Jobs: []Job{{Status: tc.status, Stale: tc.stale}}}
			if got := ShouldQuit([]PR{p}, tc.mode); got != tc.want {
				t.Fatalf("got %v want %v", got, tc.want)
			}
		})
	}
	for _, mode := range []string{"builds-finished", "all-passing", "all-closed"} {
		if ShouldQuit(nil, mode) {
			t.Fatal("empty list quit")
		}
	}
	p := PR{Fresh: true, State: "OPEN"}
	if ShouldQuit([]PR{p}, "all-passing") || ShouldQuit([]PR{p}, "builds-finished") {
		t.Fatal("no checks is not done")
	}
	p.State = "CLOSED"
	p.Removed = true
	if ShouldQuit([]PR{p}, "all-closed") {
		t.Fatal("only removed PRs quit")
	}
}
func TestHistoryRerunsAndMergeObservation(t *testing.T) {
	now := time.Now()
	ref, _ := ParseRef("acme/api#1", "github.com")
	p := NewPR(ref, now)
	next := PR{Ref: ref, Head: "a", State: "OPEN", Jobs: []Job{{Key: "test", RunID: "1", Status: "running"}}}
	p.Apply(next, now)
	next.Jobs[0].Status = "failed"
	p.Apply(next, now)
	if len(p.History) != 1 {
		t.Fatal("status update counted as rerun")
	}
	next.Jobs[0].RunID = "2"
	p.Apply(next, now)
	if len(p.History) != 2 {
		t.Fatal("rerun not counted")
	}
	next.State = "MERGED"
	p.Apply(next, now)
	if p.MergeNote != "merged with failing checks" {
		t.Fatal(p.MergeNote)
	}
	next.Jobs[0].Status = "passed"
	p.Apply(next, now)
	if p.MergeNote != "merged with failing checks" {
		t.Fatal("merge observation changed")
	}
	p.Apply(PR{Error: "offline"}, now)
	if p.Fresh || len(p.Jobs) != 1 || p.Jobs[0].Status != "passed" {
		t.Fatal("poll error erased snapshot")
	}
	fresh := NewPR(ref, now)
	fresh.Apply(next, now)
	if fresh.MergeNote != "" {
		t.Fatal("inferred historical merge")
	}
}

func TestSnapshotTimesAndOutdatedResponses(t *testing.T) {
	source := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	fetched := source.Add(time.Hour)
	p := PR{State: "OPEN", Details: PRDetails{UpdatedAt: source}, LastSuccess: fetched, Fresh: true}
	for _, next := range []PR{{Error: "offline"}, {State: "OPEN", Details: PRDetails{UpdatedAt: source.Add(-time.Minute)}}} {
		p.Apply(next, fetched.Add(time.Minute))
		if p.Fresh || p.Error == "" || !p.Details.UpdatedAt.Equal(source) || !p.LastSuccess.Equal(fetched) {
			t.Fatal("failed/older response changed displayed data timestamps")
		}
	}
	p.Apply(PR{State: "MERGED", Details: PRDetails{UpdatedAt: source.Add(time.Minute)}, LastSuccess: fetched.Add(2 * time.Minute)}, fetched.Add(3*time.Minute))
	if p.State != "MERGED" || !p.Fresh || !p.LastSuccess.Equal(fetched.Add(2*time.Minute)) {
		t.Fatal("successful merge snapshot not applied")
	}
	p.Apply(PR{State: "OPEN", Details: PRDetails{UpdatedAt: source.Add(2 * time.Minute)}}, fetched.Add(4*time.Minute))
	if p.State != "MERGED" || p.Fresh || p.Error == "" {
		t.Fatal("merged PR reverted to open")
	}
}
func TestRestoreRefreshAndPermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state", "session.json")
	ref, _ := ParseRef("acme/api#1", "github.com")
	p := NewPR(ref, time.Now())
	p.Fresh = true
	p.State = "MERGED"
	p.Monitored = 20 * time.Minute
	p.History["build\x001"] = Job{Key: "build", RunID: "1", Monitored: time.Minute}
	if err := SaveSession(path, []PR{p}); err != nil {
		t.Fatal(err)
	}
	prs, err := LoadSession(path)
	if err != nil || len(prs) != 1 {
		t.Fatalf("%v %v", prs, err)
	}
	if prs[0].Fresh || ShouldQuit(prs, "all-closed") || prs[0].Monitored != p.Monitored {
		t.Fatal("restore trusted stale state")
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0600 {
		t.Fatal("session not private")
	}
	os.WriteFile(path, []byte("broken"), 0600)
	if _, err := LoadSession(path); err == nil {
		t.Fatal("ignored corrupt session")
	}
}
func TestFuzzySortAndSummary(t *testing.T) {
	if !Fuzzy("apl142", "acme/platform #142") || Fuzzy("142apl", "acme/platform #142") {
		t.Fatal("fuzzy order incorrect")
	}
	prs := []PR{{Ref: Ref{Repo: "z/api", Number: 1}, State: "OPEN", Fresh: true, Jobs: []Job{{Progress: .2, Status: "running"}}}, {Ref: Ref{Repo: "a/ui", Number: 2}, State: "MERGED", Fresh: true, Jobs: []Job{{Progress: 1, Status: "passed"}}}}
	if got := Sorted(prs, "", "repo", false); got[0] != 1 {
		t.Fatal(got)
	}
	if got := Sorted(prs, "", "progress", false); got[0] != 0 {
		t.Fatal(got)
	}
	if got := Sorted(prs, "", "progress", true); got[0] != 1 {
		t.Fatal(got)
	}
	if got := Sorted(prs, "a/ui", "repo", false); len(got) != 1 {
		t.Fatal(got)
	}
	summary := Summary(prs, time.Minute)
	if !strings.Contains(summary, "merged") || !strings.Contains(summary, "1m0s") {
		t.Fatal(summary)
	}
	if strings.Contains(Clean("oops\x1b[2J\n"), "\x1b") {
		t.Fatal("terminal escape preserved")
	}
}
