package tui

import (
	"context"
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/mikeoertli/github-pr-monitor/internal/core"
)

func TestJiraColumnsGroupingAndActions(t *testing.T) {
	m := demoModel()
	m.Config.NoColor = true
	m.Config.Jira.BaseURL = "https://jira.example.com/team/"
	m.Config.Jira.ProjectPrefixes = []string{"ABC"}
	m.PRs[0].Title = "abc-42: Improve caching"
	m.PRs[1].Title = "ABC-42: Update client"
	m.PRs[0].Details.BaseBranch = "release/1"
	m.Config.Sort = "jira,target,repo"
	m.Update(tea.WindowSizeMsg{Width: 200, Height: 45})
	view := m.View()
	for _, want := range []string{"TARGET", "JIRA", "release/1", "── ABC-42 ──", "No Jira ticket", "[J] Open Jira", "[K] Copy Jira URL"} {
		if !strings.Contains(view, want) {
			t.Errorf("missing %q", want)
		}
	}
	if strings.Count(view, "── ABC-42 ──") != 1 {
		t.Fatal("ticket split across groups")
	}
	if strings.Contains(view, "\x1b") {
		t.Fatal("no-color emitted escapes")
	}
	var copied, opened string
	m.Actions.Copy = func(_ context.Context, s string) error { copied = s; return nil }
	m.Actions.Open = func(_ context.Context, s string) error { opened = s; return nil }
	for _, action := range []string{"J", "K"} {
		_, cmd := m.Update(key(action))
		if cmd == nil {
			t.Fatal(m.notice)
		}
		m.Update(cmd())
	}
	want := "https://jira.example.com/team/browse/ABC-42"
	if copied != want || opened != want {
		t.Fatalf("%q %q", copied, opened)
	}
	m.Config.Jira.BaseURL = ""
	if _, cmd := m.Update(key("J")); cmd != nil || !strings.Contains(m.notice, "base_url") {
		t.Fatal(m.notice)
	}
	m.Config.Jira.ProjectPrefixes = []string{"UNLISTED"}
	if m.showJira() {
		t.Fatal("unconfigured prefix displayed")
	}
	if _, cmd := m.Update(key("K")); cmd != nil {
		t.Fatal("copied unrecognized ticket")
	}
	m.Config.Jira.ProjectPrefixes = nil
	for _, size := range []struct{ w, h int }{{200, 40}, {140, 30}, {100, 24}, {80, 24}, {45, 18}, {20, 8}} {
		m.Update(tea.WindowSizeMsg{Width: size.w, Height: size.h})
		lines := strings.Split(m.View(), "\n")
		if len(lines) > size.h {
			t.Fatal("height overflow")
		}
		for _, line := range lines {
			if ansi.StringWidth(line) > size.w {
				t.Fatal("width overflow")
			}
		}
	}
}

func updateModel() *Model {
	m := demoModel()
	m.width = 200
	m.Demo = false
	m.PRs = m.PRs[:1]
	m.PRs[0].Details.ViewerCanUpdateBranch = true
	m.PRs[0].Details.BaseBranch = "main"
	m.PRs[0].State = "OPEN"
	m.Config.AutoQuit = "all-passing"
	m.PRs[0].Jobs[0].Status = "passed"
	return m
}

func TestBranchUpdateConfirmationAndRefresh(t *testing.T) {
	m := updateModel()
	calls := 0
	before := m.PRs[0]
	m.Actions.UpdateBranch = func(_ context.Context, p core.PR) error {
		calls++
		if p.Ref != before.Ref || p.Head != before.Head {
			t.Fatal("wrong PR/head captured")
		}
		return nil
	}
	m.Update(key("u"))
	if calls != 0 || m.mode != "update" {
		t.Fatal("update skipped confirmation")
	}
	view := m.View()
	for _, s := range []string{"UPDATE acme/platform #142", "merge main into feature/improvement-142?", "[Esc] Cancel"} {
		if !strings.Contains(view, s) {
			t.Fatal(view)
		}
	}
	m.Update(key("esc"))
	if m.mode != "" || calls != 0 {
		t.Fatal("cancel sent update")
	}
	m.Update(key("u"))
	if cmd := m.poll(); cmd != nil {
		t.Fatal("poll interfered with confirmation")
	}
	_, cmd := m.Update(key("enter"))
	if cmd == nil || !m.updating {
		t.Fatal("confirmation did not start update")
	}
	if m.poll() != nil {
		t.Fatal("poll during update")
	}
	m.Update(pollMsg{PRs: []core.PR{before}})
	if m.QuitReason != "" {
		t.Fatal("auto-quit during update")
	}
	msg := cmd()
	if calls != 1 {
		t.Fatal(calls)
	}
	_, refresh := m.Update(msg)
	if refresh == nil || m.PRs[0].Fresh || m.pendingUpdates[before.Ref.URL] != before.Head {
		t.Fatal("missing post-update refresh")
	}
	m.Update(pollMsg{PRs: []core.PR{before}})
	if m.QuitReason != "" || m.PRs[0].Fresh {
		t.Fatal("previous checks triggered auto-quit")
	}
	if !strings.Contains(m.View(), "Branch update pending") {
		t.Fatal("pending indicator missing")
	}
	next := before
	next.Head = "new-head"
	next.Details.ViewerCanUpdateBranch = false
	next.Jobs = append([]core.Job(nil), before.Jobs...)
	next.Jobs[0].Status = "running"
	m.Update(pollMsg{PRs: []core.PR{next}})
	if !m.PRs[0].Fresh || len(m.pendingUpdates) != 0 || m.QuitReason != "" {
		t.Fatal("new head not refreshed")
	}
	next.Jobs[0].Status = "passed"
	m.Update(pollMsg{PRs: []core.PR{next}})
	if m.QuitReason == "" {
		t.Fatal("new build passing did not permit auto-quit")
	}
}

func TestUnavailableBranchUpdatesAndFailures(t *testing.T) {
	for _, change := range []func(*Model){
		func(m *Model) { m.Demo = true },
		func(m *Model) { m.PRs[0].Fresh = false }, func(m *Model) { m.PRs[0].State = "MERGED" },
		func(m *Model) { m.PRs[0].Details.ViewerCanUpdateBranch = false }, func(m *Model) { m.updating = true },
		func(m *Model) { m.pendingUpdates = map[string]string{m.PRs[0].Ref.URL: m.PRs[0].Head} },
	} {
		for _, action := range []string{"u", "U"} {
			m := updateModel()
			change(m)
			m.Actions.UpdateBranch = func(context.Context, core.PR) error { t.Fatal("unavailable update sent"); return nil }
			_, cmd := m.Update(key(action))
			if cmd != nil || m.mode == "update" || m.notice == "" {
				t.Fatal("unavailable update offered")
			}
		}
	}
	m := updateModel()
	m.Actions.UpdateBranch = func(context.Context, core.PR) error { return errors.New("HTTP 403") }
	m.Update(key("u"))
	_, cmd := m.Update(key("enter"))
	_, refresh := m.Update(cmd())
	if m.updating || refresh == nil || len(m.pendingUpdates) != 0 || !strings.Contains(m.notice, "403") {
		t.Fatal(m.notice)
	}
	m.busy = false
	m.PRs[0].Fresh = true
	m.Update(key("u"))
	m.PRs[0].Head = "changed"
	if _, cmd := m.Update(key("enter")); cmd != nil {
		t.Fatal("changed head accepted")
	}
}

func TestBranchUpdateWhilePollIsInFlight(t *testing.T) {
	m := updateModel()
	m.Actions.UpdateBranch = func(context.Context, core.PR) error { return nil }
	old := m.PRs[0]
	m.busy = true
	m.Update(key("u"))
	if m.mode != "update" {
		t.Fatal("refresh prevented reviewing the update")
	}
	_, cmd := m.Update(key("enter"))
	_, refresh := m.Update(cmd())
	if refresh != nil || !m.refreshAgain {
		t.Fatal("should queue refresh behind in-flight poll")
	}
	_, refresh = m.Update(pollMsg{PRs: []core.PR{old}})
	if refresh == nil || m.PRs[0].Fresh || m.QuitReason != "" {
		t.Fatal("in-flight snapshot bypassed pending update")
	}
}

func TestBranchUpdateFromSelectedRowAndDetails(t *testing.T) {
	for _, width := range []int{70, 200} {
		for _, focused := range []bool{false, true} {
			for _, action := range []string{"u", "U"} {
				m := updateModel()
				m.width = width
				m.Config.NoColor = true
				selected := m.PRs[0]
				if focused {
					m.Update(key("right"))
				}
				footer := strings.Join(m.footer(), "\n")
				for _, hint := range []string{"[u] Update branch", "[U] Update now"} {
					if !strings.Contains(footer, hint) {
						t.Fatalf("missing %q in footer: %s", hint, footer)
					}
				}
				calls := 0
				m.Actions.UpdateBranch = func(_ context.Context, p core.PR) error {
					calls++
					if p.Ref != selected.Ref || p.Head != selected.Head {
						t.Fatal("wrong selected PR/head")
					}
					return nil
				}
				_, cmd := m.Update(key(action))
				if action == "u" {
					if cmd != nil || m.mode != "update" || calls != 0 {
						t.Fatal("lowercase u must require confirmation")
					}
					_, cmd = m.Update(key("enter"))
				} else if m.mode != "" {
					t.Fatal("uppercase U must skip confirmation")
				}
				if cmd == nil || !m.updating || m.expanded[selected.Ref.URL] != focused {
					t.Fatal("update must start without changing detail expansion")
				}
				if _, duplicate := m.Update(key("U")); duplicate != nil {
					t.Fatal("duplicate update allowed")
				}
				_, refresh := m.Update(cmd())
				if calls != 1 || refresh == nil || m.pendingUpdates[selected.Ref.URL] != selected.Head {
					t.Fatal("update must refresh and track the previous head")
				}
			}
		}
	}
}

func TestTargetBranchPrefixesOnlyShortenTableLabels(t *testing.T) {
	m := updateModel()
	m.Config.NoColor = true
	m.Config.TargetBranchIgnoredPrefixes = []string{"", "support/", "support/team/"}
	for _, tc := range []struct{ branch, label string }{
		{"support/release-1", "release-1"},
		{"support/team/release-1", "release-1"},
		{"support/support/release-1", "support/release-1"},
		{"Support/release-1", "Support/release-1"},
		{"feature/support/release-1", "feature/support/release-1"},
		{"support/", "support/"},
		{"main", "main"},
	} {
		m.PRs[0].Details.BaseBranch = tc.branch
		row := m.tableRow(m.PRs[0], false, []column{{"TARGET", 40}})
		fields := strings.Fields(row)
		if fields[len(fields)-1] != tc.label || m.PRs[0].Details.BaseBranch != tc.branch {
			t.Fatalf("branch %q: unexpected table label %q", tc.branch, row)
		}
	}
	full := "support/release-1"
	m.PRs[0].Details.BaseBranch = full
	if strings.Contains(m.tableRow(m.PRs[0], false, []column{{"TARGET", 40}}), "support/") {
		t.Fatal("table prefix not hidden")
	}
	if !strings.Contains(strings.Join(m.details(m.PRs[0], true), "\n"), full) {
		t.Fatal("details lost full target branch")
	}
	m.SetFilter("support/")
	if m.selected() < 0 {
		t.Fatal("filter no longer matches full target")
	}
	var updated string
	m.Actions.UpdateBranch = func(_ context.Context, p core.PR) error { updated = p.Details.BaseBranch; return nil }
	m.Update(key("u"))
	if !strings.Contains(strings.Join(m.footer(), "\n"), full) {
		t.Fatal("confirmation lost full target branch")
	}
	_, cmd := m.Update(key("enter"))
	if cmd == nil {
		t.Fatal("missing update command")
	}
	cmd()
	if updated != full {
		t.Fatal("update used shortened branch")
	}
}
