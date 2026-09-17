package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/mikeoertli/github-pr-monitor/internal/core"
)

func TestDetailsFocusNavigation(t *testing.T) {
	m := demoModel()
	m.Config.NoColor = true
	m.filter = "acme"
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 24})
	i := m.selected()
	url := m.PRs[i].Ref.URL
	m.Update(key("enter"))
	if !strings.Contains(m.View(), "↓ More details") {
		t.Fatal("clipped inline details lack a scroll hint")
	}
	m.Update(key("right"))
	if m.detailFocus != url || !m.expanded[url] {
		t.Fatal("right did not enter details")
	}
	view := m.View()
	for _, hint := range []string{"DETAILS · 1–", "↑ top", "↓ more", "[←/Esc] Back"} {
		if !strings.Contains(view, hint) {
			t.Fatalf("missing focused hint %q", hint)
		}
	}
	m.Update(key("down"))
	if m.detailScroll != 1 || m.selected() != i {
		t.Fatal("down changed PR instead of scrolling details")
	}
	m.Update(key("pgdown"))
	if m.detailScroll <= 1 || !strings.Contains(m.View(), "↑ more") {
		t.Fatal("page down did not scroll")
	}
	m.Update(key("end"))
	end := m.detailScroll
	if !strings.Contains(m.View(), "↓ end") {
		t.Fatal("end of details not indicated")
	}
	m.Update(key("down"))
	if m.detailScroll != end {
		t.Fatal("scrolled past end")
	}
	m.Update(key("home"))
	m.Update(key("up"))
	if m.detailScroll != 0 {
		t.Fatal("scrolled above beginning")
	}
	m.Update(key("esc"))
	if m.detailFocus != "" || m.filter != "acme" || !m.expanded[url] {
		t.Fatal("leaving details cleared filter or expansion")
	}
	m.Update(key("down"))
	if m.selected() == i {
		t.Fatal("table navigation not restored")
	}
	m.Update(key("right"))
	m.Update(key("left"))
	if m.detailFocus != "" {
		t.Fatal("left did not leave details")
	}
	m.Update(key("right"))
	m.Update(key("/"))
	if m.mode != "filter" || m.detailFocus != "" {
		t.Fatal("slash did not switch to filtering")
	}
}

func TestDetailsFocusFollowsPRAndHandlesRemoval(t *testing.T) {
	m := demoModel()
	i := m.selected()
	url := m.PRs[i].Ref.URL
	m.Update(key("right"))
	m.Update(key("r"))
	if m.selected() != i || m.detailFocus != url {
		t.Fatal("sort changed focused PR")
	}
	m.Update(pollMsg{PRs: DemoSnapshots([]core.Ref{m.PRs[i].Ref}, 2)})
	if m.selected() != i || m.detailFocus != url {
		t.Fatal("refresh changed focused PR")
	}
	m.Update(key("x"))
	if m.detailFocus != "" || m.selected() == i {
		t.Fatal("dismissal left focus on removed PR")
	}
	m.Update(key("right"))
	m.PRs[m.selected()].Removed = true // Retention expiry also removes rows.
	m.View()
	if m.detailFocus != "" {
		t.Fatal("removed row retained focus")
	}
}

func TestFocusedDetailsResizeAndFooter(t *testing.T) {
	m := demoModel()
	m.Config.NoColor = true
	m.Update(key("right"))
	for _, size := range []struct{ w, h int }{{200, 40}, {100, 24}, {80, 24}, {60, 18}, {20, 8}, {140, 35}} {
		m.Update(tea.WindowSizeMsg{Width: size.w, Height: size.h})
		m.Update(key("end"))
		view := m.View()
		if len(strings.Split(view, "\n")) > size.h {
			t.Fatal("focused view exceeded terminal height")
		}
		for _, line := range strings.Split(view, "\n") {
			if ansi.StringWidth(line) > size.w {
				t.Fatal("focused view exceeded terminal width")
			}
		}
	}
	for _, focused := range []bool{true, false} {
		if !focused {
			m.Update(key("left"))
		}
		footer := strings.Join(m.footer(), "\n")
		for _, heading := range []string{"PRS", "INSPECT", "COPY", "WATCH"} {
			if !strings.Contains(footer, heading) {
				t.Fatalf("missing footer heading %s", heading)
			}
		}
		if strings.Contains(footer, "[ / ]") || strings.Contains(footer, "\x1b") {
			t.Fatal("ambiguous scroll hint or styling in plain footer")
		}
	}
}
