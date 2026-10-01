package tui

import (
	"os"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
)

func TestPressedMenuStylingAndExpiry(t *testing.T) {
	old := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(old) })
	t.Setenv("TERM", "xterm-256color")
	t.Setenv("NO_COLOR", "")
	os.Unsetenv("NO_COLOR")
	for _, demo := range []bool{false, true} {
		m := demoModel()
		m.Demo = demo
		m.Update(key("s"))
		styled := m.shortcut("[s] Sort")
		if !strings.Contains(styled, "48;2;255;79;163") || !strings.Contains(styled, "38;2;0;0;0") {
			t.Fatalf("missing pink/black: %q", styled)
		}
		if ansi.Strip(styled) != "[s] Sort" {
			t.Fatal("shortcut changed")
		}
		if strings.Contains(m.shortcut("[r] Reverse"), "48;2;255;79;163") {
			t.Fatal("unpressed item highlighted")
		}
		if strings.Contains(m.View(), "KEYS") {
			t.Fatal("history overlay remains")
		}
		m.menuKeyUntil = time.Now().Add(-time.Second)
		if strings.Contains(m.shortcut("[s] Sort"), "48;2;255;79;163") {
			t.Fatal("highlight did not expire")
		}
		m.Update(key("s"))
		m.Config.NoColor = true
		if strings.Contains(m.View(), "\x1b") {
			t.Fatal("no-color emitted styling")
		}
	}
}

func TestMenuFeedbackFollowsNavigationAndIgnoresTyping(t *testing.T) {
	m := demoModel()
	m.Update(tea.KeyMsg{Type: tea.KeyRight})
	if !m.menuPressed("→", time.Now()) || !strings.Contains(strings.Join(m.footer(), "\n"), "Read details") {
		t.Fatal("focused view hides pressed action")
	}
	m.Update(tea.KeyMsg{Type: tea.KeyLeft})
	if !m.menuPressed("←/Esc", time.Now()) {
		t.Fatal("back shortcut not matched")
	}
	m.Update(key("/"))
	m.Update(key("s"))
	m.Update(key("r"))
	if m.menuKey != "/" {
		t.Fatal("filter typing highlighted a command")
	}
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if m.menuPressed("Enter", time.Now()) {
		t.Fatal("apply filter highlighted Details")
	}
	m.Update(key("?"))
	if !m.menuPressed("?", time.Now()) || !strings.Contains(m.View(), "[?] Help") {
		t.Fatal("help hides pressed action")
	}
	m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if m.menuPressed("←/Esc", time.Now()) {
		t.Fatal("close-help highlighted table Back")
	}
	for _, key := range []string{"up", "down", "pgup", "pgdown"} {
		m.recordMenuKey(key)
		if !m.menuPressed("↑↓/PgUp/PgDn", time.Now()) {
			t.Fatal(key)
		}
	}
}

func TestQuitAllowsMenuFeedbackBeforeExit(t *testing.T) {
	t.Setenv("TERM", "xterm-256color")
	t.Setenv("NO_COLOR", "")
	os.Unsetenv("NO_COLOR")
	m := demoModel()
	_, cmd := m.Update(key("q"))
	if cmd == nil || !m.menuPressed("q", time.Now()) {
		t.Fatal("quit did not leave a highlighted frame")
	}
	before := m.Config.Sort
	m.Update(key("s"))
	if m.Config.Sort != before {
		t.Fatal("input handled during exit")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatal("quit command did not exit")
	}
}
