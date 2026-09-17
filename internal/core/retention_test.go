package core

import (
	"path/filepath"
	"testing"
	"time"
)

func TestCompletedRetentionUsesClosureTime(t *testing.T) {
	now := time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)
	for _, state := range []string{"MERGED", "CLOSED"} {
		p := PR{State: state, Fresh: true, Details: PRDetails{MergedAt: now.Add(-23 * time.Hour), ClosedAt: now.Add(-23 * time.Hour)}}
		if p.RetentionExpired(now, 24*time.Hour) {
			t.Fatal("expired early")
		}
		if !p.RetentionExpired(now.Add(time.Hour), 24*time.Hour) {
			t.Fatal("did not expire at boundary")
		}
		if p.RetentionExpired(now.Add(365*24*time.Hour), -1) {
			t.Fatal("forever expired")
		}
		if !p.RetentionExpired(now, 0) {
			t.Fatal("zero retention did not expire")
		}
		p.Fresh = false
		if p.RetentionExpired(now.Add(48*time.Hour), time.Hour) {
			t.Fatal("unverified restore expired")
		}
		p.Fresh = true
		p.Error = "offline"
		if p.RetentionExpired(now.Add(48*time.Hour), time.Hour) {
			t.Fatal("failed refresh expired")
		}
	}
	p := PR{State: "OPEN", Fresh: true, Jobs: []Job{{Status: "passed"}}}
	if p.RetentionExpired(now, 0) {
		t.Fatal("passing open PR expired")
	}
	// Missing upstream timestamps use one persisted observation, not every poll.
	p.Apply(PR{State: "CLOSED"}, now)
	p.Apply(PR{State: "CLOSED"}, now.Add(23*time.Hour))
	if !p.CompletionTime().Equal(now) || !p.RetentionExpired(now.Add(24*time.Hour), 24*time.Hour) {
		t.Fatal("poll reset retention clock")
	}
	p.Apply(PR{State: "OPEN"}, now.Add(25*time.Hour))
	if !p.ClosedObservedAt.IsZero() || p.RetentionExpired(now.Add(48*time.Hour), time.Hour) {
		t.Fatal("reopened PR retained closure timer")
	}
	p.Apply(PR{State: "CLOSED"}, now.Add(26*time.Hour))
	if !p.CompletionTime().Equal(now.Add(26 * time.Hour)) {
		t.Fatal("new closure did not restart fallback timer")
	}
}

func TestRetentionAndDismissalsPersist(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.json")
	var prs []PR
	for n, raw := range []string{"acme/api#1", "acme/api#2", "acme/api#3"} {
		ref, _ := ParseRef(raw, "github.com")
		p := NewPR(ref, time.Now())
		p.State = "MERGED"
		p.ClosedObservedAt = time.Now().Add(-time.Hour)
		p.Removed = n > 0
		p.Expired = n == 2
		prs = append(prs, p)
	}
	if err := SaveSession(path, prs); err != nil {
		t.Fatal(err)
	}
	s, err := LoadSessionState(path)
	if err != nil || len(s.PRs) != 1 || len(s.Dismissed) != 1 || s.Dismissed[0].Number != 2 || !s.PRs[0].ClosedObservedAt.Equal(prs[0].ClosedObservedAt) {
		t.Fatalf("bad restore: %+v %v", s, err)
	}
	if s.PRs[0].Fresh {
		t.Fatal("restored data trusted without refreshing")
	}
	if err := SaveSession(path, s.PRs, s.Dismissed...); err != nil {
		t.Fatal(err)
	}
	s, err = LoadSessionState(path)
	if err != nil || len(s.Dismissed) != 1 {
		t.Fatal("dismissal lost at next save")
	}
	// An explicit add removes the dismissal, even when an old record is supplied.
	s.PRs = append(s.PRs, NewPR(s.Dismissed[0], time.Now()))
	if err := SaveSession(path, s.PRs, s.Dismissed...); err != nil {
		t.Fatal(err)
	}
	s, err = LoadSessionState(path)
	if err != nil || len(s.Dismissed) != 0 || len(s.PRs) != 2 {
		t.Fatal("explicit re-add did not clear dismissal")
	}
}
