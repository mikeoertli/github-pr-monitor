package tui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mikeoertli/github-pr-monitor/internal/core"
)

func TestCompletedRowsRetainedExpiredAndDismissed(t *testing.T) {
	m := demoModel()
	m.filter = "platform"
	i := m.selected()
	p := m.PRs[i]
	p.State = "MERGED"
	p.Details.MergedAt = time.Now().Add(-time.Hour)
	m.Update(pollMsg{PRs: []core.PR{p}})
	if m.PRs[i].Removed {
		t.Fatal("recent merge disappeared")
	}
	if detail := strings.Join(m.details(m.PRs[i], true), "\n"); !strings.Contains(detail, "Kept until:") || !strings.Contains(detail, "Dismiss sooner") {
		t.Fatal("retention controls missing")
	}
	m.Config.CompletedRetention = "forever"
	m.ExpireCompleted(time.Now().Add(365 * 24 * time.Hour))
	if m.PRs[i].Removed {
		t.Fatal("forever mode expired")
	}
	m.Update(key("x"))
	if !m.PRs[i].Removed || m.PRs[i].Expired || len(m.Dismissed) != 1 {
		t.Fatal("dismissal not recorded")
	}
	m.Update(importMsg{Refs: []core.Ref{p.Ref}, Label: "discovery"})
	if !m.PRs[i].Removed {
		t.Fatal("discovery resurrected dismissed PR")
	}
	m.Update(importMsg{Refs: []core.Ref{p.Ref}, Label: "clipboard"})
	if m.PRs[i].Removed || len(m.Dismissed) != 0 {
		t.Fatal("explicit re-add failed")
	}
	m.Config.CompletedRetention = "24h"
	p.Details.MergedAt = time.Now().Add(-25 * time.Hour)
	m.Update(pollMsg{PRs: []core.PR{p}})
	if !m.PRs[i].Expired || !m.PRs[i].Removed || len(m.Dismissed) != 0 {
		t.Fatal("expiry confused with dismissal")
	}
	if !strings.Contains(core.Summary(m.PRs, time.Minute), "retention expired") {
		t.Fatal("expiry removed summary history")
	}
}

func TestRetentionKeepsAutoQuitPolicy(t *testing.T) {
	for _, retention := range []string{"24h", "forever", "0s"} {
		m := demoModel()
		m.PRs = m.PRs[:1]
		m.Config.AutoQuit = "all-closed"
		m.Config.CompletedRetention = retention
		p := m.PRs[0]
		p.State = "MERGED"
		p.Details.MergedAt = time.Now().Add(-time.Minute)
		_, cmd := m.Update(pollMsg{PRs: []core.PR{p}})
		if cmd == nil {
			t.Fatalf("%s blocked auto-quit", retention)
		}
		if _, ok := cmd().(tea.QuitMsg); !ok {
			t.Fatal("expected auto-quit")
		}
		if retention != "0s" && m.PRs[0].Removed {
			t.Fatal("auto-quit discarded retained row")
		}
	}
}
