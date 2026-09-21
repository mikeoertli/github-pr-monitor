package tui

import (
	"context"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/mikeoertli/github-pr-monitor/internal/config"
	"github.com/mikeoertli/github-pr-monitor/internal/core"
)

func demoModel() *Model {
	c := config.Defaults()
	c.AutoQuit = "never"
	m := New(context.Background(), c, DemoPRs(), nil, Actions{}, "", true)
	refs := []core.Ref{}
	for _, p := range m.PRs {
		refs = append(refs, p.Ref)
	}
	m.Update(pollMsg{PRs: DemoSnapshots(refs, 1)})
	return m
}
func key(s string) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)} }
func TestLayoutAndCIColumn(t *testing.T) {
	m := demoModel()
	if !m.showCI() {
		t.Fatal("mixed providers hidden")
	}
	for _, size := range []struct{ w, h int }{{140, 35}, {100, 24}, {80, 24}, {45, 18}, {20, 8}} {
		m.Update(tea.WindowSizeMsg{Width: size.w, Height: size.h})
		view := m.View()
		lines := strings.Split(view, "\n")
		if len(lines) > size.h {
			t.Fatalf("height %d > %d", len(lines), size.h)
		}
		for _, line := range lines {
			if ansi.StringWidth(line) > size.w {
				t.Fatalf("width %d > %d: %s", ansi.StringWidth(line), size.w, line)
			}
		}
	}
	m.PRs = m.PRs[:1]
	if m.showCI() {
		t.Fatal("single provider column shown")
	}
}
func TestFilterAndAutoQuitUseMatchingPRs(t *testing.T) {
	m := demoModel()
	if rows := core.Sorted(m.PRs, "plat", "repo", false); len(rows) != 1 || rows[0] != 0 {
		t.Fatalf("filter leaked across fields: %v", rows)
	}
	m.Config.AutoQuit = "all-closed"
	m.SetFilter("docs")
	_, filteredQuit := m.Update(pollMsg{})
	if filteredQuit == nil || m.QuitReason == "" {
		t.Fatal("matching merged PR should allow auto-quit despite hidden open PRs")
	}
	m.QuitReason = ""
	m.SetFilter("no-match")
	m.Update(pollMsg{})
	if m.QuitReason != "" {
		t.Fatal("empty match set triggered quit")
	}
	m.SetFilter("")
	for i := range m.PRs {
		m.PRs[i].State = "MERGED"
		m.PRs[i].Fresh = true
	}
	_, cmd := m.Update(pollMsg{})
	if cmd == nil {
		t.Fatal("all merged did not quit")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatal("expected quit command")
	}
}
func TestInputNavigationAndRemoval(t *testing.T) {
	m := demoModel()
	baseCount := len(m.PRs)
	m.Update(key("a"))
	m.input.SetValue("acme/new#4 acme/new#4")
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if len(m.PRs) != baseCount+1 || m.mode != "" {
		t.Fatal("batch add failed")
	}
	m.Update(key("/"))
	m.input.SetValue("new")
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if m.selected() != baseCount {
		t.Fatal("filter failed")
	}
	m.Update(key("x"))
	if !m.PRs[baseCount].Removed {
		t.Fatal("remove failed")
	}
	if len(m.PRs) != baseCount+1 {
		t.Fatal("removed PR missing from summary")
	}
	m.Update(key("s"))
	if m.Config.Sort != "progress" {
		t.Fatal("sort failed")
	}
	m.Update(key("r"))
	if !m.Config.Descending {
		t.Fatal("reverse failed")
	}
}
func TestMonitoredTimePreservedAcrossPolls(t *testing.T) {
	m := demoModel()
	m.lastTick = time.Now().Add(-10 * time.Second)
	m.account(time.Now())
	var total time.Duration
	for _, j := range m.PRs[0].History {
		total += j.Monitored
	}
	if total < 10*time.Second {
		t.Fatal(total)
	}
	ref := m.PRs[0].Ref
	m.PRs[0].Apply(DemoSnapshots([]core.Ref{ref}, 2)[0], time.Now())
	var after time.Duration
	for _, j := range m.PRs[0].History {
		after += j.Monitored
	}
	if after != total {
		t.Fatal("poll reset accumulated time")
	}
}
