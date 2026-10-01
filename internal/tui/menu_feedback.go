package tui

import (
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

var menuPressedStyle = lipgloss.NewStyle().Background(lipgloss.Color("#ff4fa3")).Foreground(lipgloss.Color("#000000")).Bold(true)

const menuFlashDuration = 1500 * time.Millisecond

// Highlight shortcut keys, never letters typed into the PR/filter input.
func (m *Model) recordMenuKey(key string) {
	if m.mode != "" {
		if key == "enter" || key == "esc" {
			m.menuKey = ""
		}
		return
	}
	if m.help && (key == "esc" || key == "q") {
		m.menuKey = ""
		return
	}
	aliases := map[string]string{"right": "→", "left": "←", "up": "↑", "down": "↓", "j": "↓", "k": "↑", "pgdown": "PgDn", "pgup": "PgUp", "home": "Home", "end": "End", "enter": "Enter", "esc": "Esc", " ": "Enter", "tab": "Tab", "shift+tab": "Tab", "ctrl+v": "v", "Q": "q"}
	if alias, ok := aliases[key]; ok {
		key = alias
	}
	m.menuKey, m.menuKeyUntil = key, time.Now().Add(menuFlashDuration)
}

func (m *Model) menuPressed(keys string, now time.Time) bool {
	if m.menuKey == "" || !now.Before(m.menuKeyUntil) {
		return false
	}
	if keys == m.menuKey {
		return true
	}
	for _, key := range strings.Split(keys, "/") {
		if key == m.menuKey || (key == "↑↓" && (m.menuKey == "↑" || m.menuKey == "↓")) {
			return true
		}
	}
	return false
}
