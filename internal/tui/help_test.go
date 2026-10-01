package tui

import (
	"context"
	"os"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
)

func TestHelpStyleAndPlainTextShareContent(t *testing.T) {
	old := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(old) })
	t.Setenv("TERM", "xterm-256color")
	t.Setenv("NO_COLOR", "")
	os.Unsetenv("NO_COLOR")
	m := demoModel()
	for _, width := range []int{200, 120, 80, 40} {
		m.width, m.height = width, 80
		m.Config.NoColor = false
		styled := m.helpView()
		if !strings.Contains(styled, "\x1b[") {
			t.Fatal("help lacks styling")
		}
		m.Config.NoColor = true
		plain := m.helpView()
		if strings.Contains(plain, "\x1b") || ansi.Strip(styled) != plain {
			t.Fatal("styling changed help content or wrapping")
		}
		for _, line := range strings.Split(styled, "\n") {
			if ansi.StringWidth(line) > width {
				t.Fatalf("help exceeds width %d: %q", width, line)
			}
		}
		if width < 120 {
			// Wrapped explanations remain readable words, even across lines.
			body := strings.Join(strings.Fields(strings.Join(m.helpBody(), " ")), " ")
			for _, phrase := range []string{"project_prefixes can restrict title matches.", "unticketed PRs follow.", "failed refresh."} {
				if !strings.Contains(body, phrase) {
					t.Fatalf("help at width %d splits words in %q", width, phrase)
				}
			}
		}
	}
}

func TestHelpScrollingReachesAllSectionsAndPreservesSelection(t *testing.T) {
	m := demoModel()
	m.Config.NoColor = true
	m.Update(key("down"))
	m.Update(key("right"))
	selected, focus, details := m.selected(), m.detailFocus, m.detailScroll
	for _, size := range []struct{ w, h int }{{200, 30}, {80, 24}, {40, 12}, {20, 8}} {
		m.Update(tea.WindowSizeMsg{Width: size.w, Height: size.h})
		m.Update(key("?"))
		var seen strings.Builder
		for {
			view := m.View()
			if len(strings.Split(view, "\n")) > size.h {
				t.Fatal("help exceeds height")
			}
			for _, line := range strings.Split(view, "\n") {
				if ansi.StringWidth(line) > size.w {
					t.Fatal("help exceeds width")
				}
			}
			seen.WriteString(view)
			before := m.helpScroll
			m.Update(key("down"))
			if m.helpScroll == before {
				break
			}
		}
		for _, heading := range []string{"BROWSE", "COPY", "PRS", "INSPECT", "WATCH", "CONFIGURATION", "LEGEND", "OTHER SHORTCUTS"} {
			if !strings.Contains(seen.String(), heading) {
				t.Fatalf("%dx%d hides %s", size.w, size.h, heading)
			}
		}
		m.Update(key("home"))
		if m.helpScroll != 0 {
			t.Fatal("home did not reach start")
		}
		m.Update(key("pgdown"))
		m.Update(key("end"))
		if !strings.Contains(m.View(), "monitoring summary") && size.w >= 40 {
			t.Fatal("end did not reach final help content")
		}
		m.Update(key("pgup"))
		m.Update(key("esc"))
		if m.help || m.selected() != selected || m.detailFocus != focus || m.detailScroll != details {
			t.Fatal("help navigation changed PR/details navigation")
		}
	}
	// Viewing help cannot accidentally perform a browser, copy, or update action.
	m.Actions.Open = func(_ context.Context, _ string) error { t.Fatal("help opened a browser"); return nil }
	m.Actions.Copy = func(_ context.Context, _ string) error { t.Fatal("help copied data"); return nil }
	m.Update(key("?"))
	for _, action := range []string{"g", "c", "G", "C", "u", "U"} {
		if _, cmd := m.Update(key(action)); cmd != nil {
			t.Fatalf("help dispatched %s", action)
		}
	}
	m.Update(key("q"))
	m.Update(key("?"))
	if m.helpScroll != 0 {
		t.Fatal("new help session did not start at top")
	}
}
