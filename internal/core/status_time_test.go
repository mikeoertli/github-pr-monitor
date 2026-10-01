package core

import (
	"testing"
	"time"
)

func TestStatusAgeUsesProviderTimesAndSurvivesRefresh(t *testing.T) {
	now := time.Date(2026, 9, 30, 15, 0, 0, 0, time.UTC)
	ended := now.Add(-90 * time.Minute)
	next := PR{State: "OPEN", Head: "a", Jobs: []Job{{Status: "failed", CompletedAt: ended}}}
	p := NewPR(Ref{}, now)
	p.Apply(next, now)
	if p.StatusAge(now) != "90m ago" || p.StatusTimeObserved {
		t.Fatalf("%+v", p)
	}
	p.Apply(next, now.Add(time.Minute))
	if !p.StatusChangedAt.Equal(ended) {
		t.Fatal("refresh changed status time")
	}
	p.Apply(PR{Error: "offline"}, now.Add(2*time.Minute))
	if !p.StatusChangedAt.Equal(ended) {
		t.Fatal("failed fetch changed status time")
	}
	p.Apply(next, now.Add(3*time.Minute))
	if !p.StatusChangedAt.Equal(ended) {
		t.Fatal("recovery changed status time")
	}
	next.State = "MERGED"
	next.Details.MergedAt = now.Add(-20 * time.Minute)
	p.Apply(next, now)
	if p.DisplayStatus() != "merged" || p.StatusAge(now) != "20m ago" {
		t.Fatalf("%+v", p)
	}
}

func TestStatusAgeObservedFallbackAndTransition(t *testing.T) {
	now := time.Now()
	p := NewPR(Ref{}, now)
	next := PR{State: "OPEN", Head: "a", Details: PRDetails{Mergeable: "CONFLICTING"}}
	p.Apply(next, now)
	next.Details.UpdatedAt = now.Add(time.Minute)
	p.Apply(next, now.Add(time.Minute))
	p.Apply(PR{Error: "offline"}, now.Add(2*time.Minute))
	p.Apply(next, now.Add(3*time.Minute))
	if !p.StatusChangedAt.Equal(now) || !p.StatusTimeObserved || p.StatusAge(now.Add(90*time.Minute)) != "~90m ago" {
		t.Fatalf("%+v", p)
	}
	next.Details.Mergeable = "MERGEABLE"
	p.Apply(next, now.Add(4*time.Minute))
	if !p.StatusChangedAt.Equal(now.Add(4 * time.Minute)) {
		t.Fatal("transition not recorded")
	}
	next.Head = "b"
	p.Apply(next, now.Add(5*time.Minute))
	if !p.StatusChangedAt.Equal(now.Add(5 * time.Minute)) {
		t.Fatal("new head did not reset observed status")
	}
}

func TestAggregateStatusTimeAndRelativeBoundaries(t *testing.T) {
	now := time.Now()
	early, late := now.Add(-90*time.Minute), now.Add(-20*time.Minute)
	p := PR{State: "OPEN", Jobs: []Job{{Status: "failed", CompletedAt: early}, {Status: "passed", CompletedAt: late}}}
	if !p.StatusTime().Equal(late) {
		t.Fatal("aggregate becomes terminal only when all checks finish")
	}
	p.Jobs[1].CompletedAt = time.Time{}
	if !p.StatusTime().IsZero() {
		t.Fatal("invented aggregate event time")
	}
	p.State = "CLOSED"
	p.Details.ClosedAt = early
	if !p.StatusTime().Equal(early) {
		t.Fatal("closure should win")
	}
	for _, tc := range []struct {
		ago  time.Duration
		want string
	}{{-time.Minute, "just now"}, {0, "just now"}, {59 * time.Second, "just now"}, {time.Minute, "1m ago"}, {90 * time.Minute, "90m ago"}, {2 * time.Hour, "2h ago"}, {48 * time.Hour, "2d ago"}} {
		if got := RelativeTime(now.Add(-tc.ago), now); got != tc.want {
			t.Errorf("%s: %s", tc.ago, got)
		}
	}
	if RelativeTime(time.Time{}, now) != "" {
		t.Fatal("unknown time should stay unknown")
	}
}

func TestStatusTimeSurvivesSessionRestore(t *testing.T) {
	path := t.TempDir() + "/session.json"
	now := time.Now().Truncate(time.Second)
	p := NewPR(Ref{URL: "https://github.com/acme/api/pull/1", Repo: "acme/api", Host: "github.com", Number: 1}, now)
	next := PR{Ref: p.Ref, State: "OPEN", Head: "a", Jobs: []Job{{Status: "failed"}}}
	p.Apply(next, now)
	if err := SaveSession(path, []PR{p}); err != nil {
		t.Fatal(err)
	}
	s, err := LoadSession(path)
	if err != nil {
		t.Fatal(err)
	}
	restored := s[0]
	restored.Apply(next, now.Add(time.Hour))
	if !restored.StatusChangedAt.Equal(now) || !restored.StatusTimeObserved {
		t.Fatal("restore reset event age")
	}
}
