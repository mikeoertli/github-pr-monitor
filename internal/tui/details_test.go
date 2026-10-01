package tui

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/mikeoertli/github-pr-monitor/internal/core"
)

func TestDetailsWarningsAndResponsiveLayout(t *testing.T) {
	m := demoModel()
	m.Config.NoColor = true
	m.filter = "platform"
	p := &m.PRs[m.selected()]
	p.Jobs[0].Warning = "Could not retrieve Jenkins build: HTTP 403"
	m.Update(tea.WindowSizeMsg{Width: 200, Height: 40})
	view := m.View()
	for _, want := range []string{"⚠ ▸", "HTTP 403", "BRANCH", "TITLE", "INSPECT", "[g] Open PR", "[c] Open CI"} {
		if !strings.Contains(view, want) {
			t.Fatalf("missing %q in view:\n%s", want, view)
		}
	}
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	view = m.View()
	for _, want := range []string{"▾", "Branch: feature/", "Author: demo-user", "Changes: +142", "Review: APPROVED", "PR URL: " + p.Ref.URL, "CI URL: " + p.Jobs[0].URL} {
		if !strings.Contains(view, want) {
			t.Fatalf("missing %q in expanded view:\n%s", want, view)
		}
	}
	// All content, including URLs, must remain reachable on a small terminal.
	p.Jobs[0].URL = "https://ci.example.com/" + strings.Repeat("segment/", 40) + "end-marker"
	m.Update(tea.WindowSizeMsg{Width: 60, Height: 18})
	m.Update(tea.KeyMsg{Type: tea.KeyRight})
	seenEnd := false
	for n := 0; n < 80; n++ {
		view = m.View()
		if strings.Contains(view, "end-marker") {
			seenEnd = true
		}
		if len(strings.Split(view, "\n")) > 18 {
			t.Fatal("overflowed terminal height")
		}
		for _, line := range strings.Split(view, "\n") {
			if ansi.StringWidth(line) > 60 {
				t.Fatal("overflowed terminal width")
			}
		}
		m.Update(tea.KeyMsg{Type: tea.KeyDown})
	}
	if !seenEnd {
		t.Fatal("could not scroll to full CI URL")
	}
	m.Update(tea.KeyMsg{Type: tea.KeyLeft}) // Close details and return to the table.
	if m.expanded[p.Ref.URL] {
		t.Fatal("collapse failed")
	}
	if !strings.Contains(m.View(), "⚠ ▸") {
		t.Fatal("collapse hid warning")
	}
	// Expansion belongs to a PR, not its row position after a sort/filter.
	m.Update(tea.KeyMsg{Type: tea.KeyRight})
	m.filter = ""
	m.Update(key("r"))
	if !m.expanded[p.Ref.URL] {
		t.Fatal("sort lost expansion")
	}
}

func TestOpenSelectedPRAndCheck(t *testing.T) {
	m := demoModel()
	m.filter = "platform"
	p := &m.PRs[m.selected()]
	second := p.Jobs[0]
	second.URL = "https://ci.example.com/job/second/12/"
	second.Name = "Second check"
	p.Jobs = append(p.Jobs, second)
	var opened string
	m.Actions.Open = func(_ context.Context, raw string) error { opened = raw; return nil }
	for _, tc := range []struct{ key, want string }{{"g", p.Ref.URL}, {"c", p.Jobs[0].URL}} {
		_, cmd := m.Update(key(tc.key))
		m.Update(cmd())
		if opened != tc.want {
			t.Fatalf("opened %q, want %q", opened, tc.want)
		}
	}
	m.Update(tea.KeyMsg{Type: tea.KeyTab})
	_, cmd := m.Update(key("c"))
	m.Update(cmd())
	if opened != second.URL {
		t.Fatal("did not open selected check")
	}
}

func TestCopyRequestMenuActions(t *testing.T) {
	m := demoModel()
	m.filter = "platform"
	var copied string
	m.Actions.Copy = func(_ context.Context, text string) error { copied = text; return nil }
	for _, tc := range []struct{ key, want, label string }{{"G", "pr view ", "gh pr view command"}, {"C", "/api/json", "Jenkins curl command"}} {
		_, cmd := m.Update(key(tc.key))
		if cmd == nil {
			t.Fatal(m.notice)
		}
		m.Update(cmd())
		if !strings.Contains(copied, tc.want) || !strings.Contains(m.notice, tc.label) {
			t.Fatal("request copy action failed")
		}
	}
	view := m.View()
	for _, hint := range []string{"[G] gh command", "[C] Jenkins curl"} {
		if !strings.Contains(view, hint) {
			t.Fatalf("menu lacks %s", hint)
		}
	}
	m.filter = "web"
	_, cmd := m.Update(key("C"))
	if cmd != nil || !strings.Contains(m.notice, "not a Jenkins") {
		t.Fatal("non-Jenkins action did not explain unavailability")
	}
	m.filter = "platform"
	m.Actions.Copy = func(context.Context, string) error { return errors.New("copy failed") }
	_, cmd = m.Update(key("G"))
	m.Update(cmd())
	if m.notice != "copy failed" {
		t.Fatal("command copy error hidden")
	}
}

func TestMergedRowAndSourceDataTimes(t *testing.T) {
	m := demoModel()
	m.filter = "platform"
	m.Config.NoColor = true
	m.Update(tea.WindowSizeMsg{Width: 200, Height: 45})
	p := &m.PRs[m.selected()]
	p.State = "MERGED"
	p.Details.Mergeable = "CONFLICTING"
	p.Details.UpdatedAt = time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	p.Details.MergedAt = p.Details.UpdatedAt
	p.LastSuccess = p.Details.UpdatedAt.Add(time.Hour)
	p.LastAttempt = p.LastSuccess.Add(5 * time.Minute)
	p.Jobs[0].Status = "passed"
	p.Jobs[0].Number = "43"
	m.expanded[p.Ref.URL] = true
	view := m.View()
	for _, want := range []string{"Mergeable: n/a (merged)", "State: merged", "GitHub data updated: " + stamp(p.Details.UpdatedAt), "Last successful data fetch: " + stamp(p.LastSuccess), "Merged: " + stamp(p.Details.MergedAt)} {
		if !strings.Contains(view, want) {
			t.Fatalf("missing %q", want)
		}
	}
	if strings.Contains(view, "CONFLICTING") {
		t.Fatal("mergeability shown for merged PR")
	}
	row := m.tableRow(*p, true, m.columns())
	if !strings.Contains(row, "merged") || !strings.Contains(row, "43") || strings.Contains(row, "passed") {
		t.Fatal("table did not prioritize PR state or show build number")
	}
	p.Apply(core.PR{Error: "offline"}, p.LastAttempt)
	view = m.View()
	if !strings.Contains(view, "Last successful data fetch: "+stamp(p.LastSuccess)) || !strings.Contains(view, "Last attempt (failed): "+stamp(p.LastAttempt)) {
		t.Fatal("failed attempt confused with data freshness")
	}
}

func TestOpenAndCommandShortcutsWhileDetailsFocused(t *testing.T) {
	m := demoModel()
	m.filter = "platform"
	m.Update(key("right"))
	m.Update(key("end"))
	selected, scroll := m.selected(), m.detailScroll
	p := m.PRs[selected]
	var opened, copied string
	m.Actions.Open = func(_ context.Context, raw string) error { opened = raw; return nil }
	m.Actions.Copy = func(_ context.Context, raw string) error { copied = raw; return nil }
	for _, tc := range []struct{ key, url, command string }{
		{"g", p.Ref.URL, ""}, {"c", p.Jobs[0].URL, ""},
		{"G", "", "pr view "}, {"C", "", "/api/json"},
	} {
		opened, copied = "", ""
		_, cmd := m.Update(key(tc.key))
		if cmd == nil {
			t.Fatalf("%s did not trigger its action: %s", tc.key, m.notice)
		}
		m.Update(cmd())
		if opened != tc.url || (tc.command == "" && copied != "") || (tc.command != "" && !strings.Contains(copied, tc.command)) {
			t.Fatalf("%s: opened %q, copied %q", tc.key, opened, copied)
		}
		if m.selected() != selected || m.detailScroll != scroll || m.detailFocus != p.Ref.URL {
			t.Fatalf("%s changed navigation instead of acting on the selected PR", tc.key)
		}
	}
}
