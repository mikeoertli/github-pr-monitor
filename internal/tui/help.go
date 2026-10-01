package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/cellbuf"
)

type helpSection struct {
	title string
	lines []string
}

func (m *Model) helpBody() []string {
	key := func(keys, action string) string { return m.paint(accent, "["+keys+"]") + " " + action }
	note := func(text string) string { return m.paint(muted, text) }
	join := func(items ...string) string { return strings.Join(items, m.paint(muted, "  ·  ")) }
	left := []helpSection{
		{"BROWSE", []string{
			join(key("g", "GitHub PR"), key("c", "Selected CI URL"), key("J", "Jira ticket")),
			note("Set [jira] base_url for links; project_prefixes can restrict title matches."),
		}},
		{"COPY", []string{
			join(key("G", "gh command"), key("C", "Jenkins curl"), key("K", "Jira URL")),
			note("Copy a runnable request for the selected PR or CI check."),
		}},
		{"PRS", []string{
			join(key("d", "Discover"), key("a", "Add"), key("x", "Dismiss")),
			join(key("/", "Filter"), key("Esc", "Clear filter"), key("s", "Sort"), key("r", "Reverse")),
			note("Filter controls display, polling and auto-quit. Discovery is unchanged."),
			note("Sort cycles repo → progress → Jira. Dismissals persist across launches."),
		}},
		{"INSPECT", []string{
			join(key("Enter/Space", "Toggle details"), key("→", "Focus"), key("←/Esc", "Back")),
			key("Tab/Shift+Tab", "Choose the CI check for details and browser actions"),
			key("↑↓/PgUp/PgDn", "Scroll focused details; Home/End jump to first/last"),
			note("Details show a line range and arrows when more content is available."),
		}},
	}
	right := []helpSection{
		{"WATCH", []string{
			join(key("↑/k/↓/j", "Select PR"), key("PgUp/PgDn", "Page"), key("Home/End", "First/last")),
			join(key("p", "Pause/resume"), key("R", "Refresh now")),
			join(key("u", "Update branch with confirmation"), key("U", "Update immediately")),
			note("↑ beside a PR means a branch update is available. Works with details collapsed."),
		}},
		{"CONFIGURATION", []string{
			m.paint(accent, "--print-config") + "  List all settings and defaults",
			m.paint(accent, "--edit-config / -e") + "  Edit settings in $EDITOR",
			m.paint(detailTitle, `sort = "jira,target,repo"`),
			note("Put top-level settings before TOML sections. Jira groups sort by target, repo, then source branch; unticketed PRs follow."),
		}},
		{"LEGEND", []string{
			join(m.paint(good, "Passed / merged / mergeable"), m.paint(bad, "Failed / conflicts")),
			m.paint(warn, "Building / pending / warnings"),
			join(key("~", "Estimated progress"), key("!", "Overdue"), key("—", "Unknown")),
			note("Progress: red → green → orange/red. ⚠ flags unavailable CI data or a failed refresh."),
			note("--no-color or NO_COLOR disables styling."),
		}},
		{"OTHER SHORTCUTS", []string{
			key("v/Ctrl+V", "Import PR links from the clipboard"),
			key("q/Q/Ctrl+C", "Quit the dashboard and print the monitoring summary"),
		}},
	}
	width := max(1, m.width)
	render := func(sections []helpSection, limit int) []string {
		var lines []string
		for i, section := range sections {
			if i > 0 {
				lines = append(lines, "")
			}
			lines = append(lines, m.paint(detailSection, section.title))
			for _, line := range section.lines {
				// Preserve formatting on both sides of a wrap, including when
				// the resulting lines are placed beside the other column.
				lines = append(lines, strings.Split(cellbuf.Wrap(line, limit, ""), "\n")...)
			}
		}
		return lines
	}
	if width < 120 {
		return render(append(left, right...), width)
	}
	// Two columns use the horizontal space without forcing a long vertical block.
	columnWidth := (width - 4) / 2
	first, second := render(left, columnWidth), render(right, width-columnWidth-4)
	lines := make([]string, 0, max(len(first), len(second)))
	for i := 0; i < max(len(first), len(second)); i++ {
		a, b := "", ""
		if i < len(first) {
			a = first[i]
		}
		if i < len(second) {
			b = second[i]
		}
		lines = append(lines, cell(a, columnWidth)+"    "+b)
	}
	return lines
}

func (m *Model) helpPageHeight() int { return max(1, m.height-3) }

func (m *Model) helpView() string {
	width := max(1, m.width)
	header := m.paint(detailTitle, "gprm") + " · " + m.shortcut("[?] Help") + "    " + m.shortcut("[Esc] Back")
	body := m.helpBody()
	pageHeight := m.helpPageHeight()
	if m.height <= 0 {
		pageHeight = len(body)
	}
	m.helpScroll = max(0, min(m.helpScroll, max(0, len(body)-pageHeight)))
	end := min(len(body), m.helpScroll+pageHeight)
	lines := []string{header, ""}
	lines = append(lines, body[m.helpScroll:end]...)
	footer := m.paint(muted, "Esc / ? / q: return to dashboard  ·  Q / Ctrl+C: quit")
	if len(body) > pageHeight {
		footer = m.paint(muted, fmt.Sprintf("HELP %d–%d/%d  ·  ", m.helpScroll+1, end, len(body))) + m.shortcut("[↑↓/PgUp/PgDn] Scroll") + "  ·  " + m.shortcut("[Home/End] First/last")
	}
	lines = append(lines, footer)
	if m.height > 0 && len(lines) > m.height {
		lines = lines[:m.height]
	}
	for i, line := range lines {
		lines[i] = ansi.Truncate(line, width, "…")
	}
	return strings.Join(lines, "\n")
}
