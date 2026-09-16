package tui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/mikeoertli/github-pr-monitor/internal/core"
)

var accent = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Dark: "#60a5fa", Light: "#1d4ed8"}).Bold(true)
var muted = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Dark: "#9ca3af", Light: "#4b5563"})
var good = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Dark: "#4ade80", Light: "#15803d"})
var bad = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Dark: "#f87171", Light: "#b91c1c"})
var warn = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Dark: "#facc15", Light: "#a16207"})

func (m *Model) paint(style lipgloss.Style, s string) string {
	if m.noColor() {
		return s
	}
	return style.Render(s)
}
func cell(s string, n int) string {
	s = ansi.Truncate(s, max(n, 1), "…")
	return s + strings.Repeat(" ", max(0, n-ansi.StringWidth(s)))
}
func providers(p core.PR) string {
	seen := map[string]bool{}
	for _, j := range p.Jobs {
		seen[j.Provider] = true
	}
	var names []string
	for n := range seen {
		names = append(names, n)
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}
func (m *Model) showCI() bool {
	if m.Config.CIColumn == "always" {
		return true
	}
	if m.Config.CIColumn == "never" {
		return false
	}
	seen := map[string]bool{}
	for _, p := range m.PRs {
		if p.Removed {
			continue
		}
		for _, j := range p.Jobs {
			seen[j.Provider] = true
		}
	}
	return len(seen) > 1
}
func primaryJob(p core.PR) *core.Job {
	for i := range p.Jobs {
		if !core.Terminal(p.Jobs[i].Status) {
			return &p.Jobs[i]
		}
	}
	if len(p.Jobs) > 0 {
		return &p.Jobs[0]
	}
	return nil
}

// footer separates actions into labeled groups and keeps keycaps readable without color.
func (m *Model) footer() []string {
	if m.mode != "" {
		return []string{m.paint(accent, m.mode+" › ") + m.input.View(), "[Enter] Apply   ·   [Esc] Cancel"}
	}
	if m.width < 80 {
		return []string{"[Enter] Details  ·  [o] PR  ·  [b] CI", "[c/C] Commands  ·  [?] Keys  ·  [q] Quit"}
	}
	groups := [][]string{
		{"PRs", "[v] Paste", "[a] Add", "[d] Discover", "[/] Filter", "[s] Sort", "[r] Reverse"},
		{"Inspect", "[Enter] Details", "[Tab] Check", "[o] Open PR", "[b] Open CI"},
		{"Copy", "[y] PR JSON", "[Y] Table JSON", "[c] gh command", "[C] Jenkins curl"},
		{"Watch", "[↑↓] Select", "[ / ] Scroll details", "[p] Pause", "[R] Refresh", "[?] Help", "[q] Quit"},
	}
	var lines []string
	for _, group := range groups {
		line := cell(group[0], 8)
		for _, hint := range group[1:] {
			if ansi.StringWidth(line)+len(hint)+5 > m.width {
				lines = append(lines, line)
				line = strings.Repeat(" ", 8)
			}
			if strings.TrimSpace(line) != "" && ansi.StringWidth(line) > 8 {
				line += "  │  "
			}
			line += hint
		}
		lines = append(lines, line)
	}
	return lines
}

type column struct {
	name  string
	width int
}

func (m *Model) columns() []column {
	w := max(20, m.width)
	cols := []column{{"REPOSITORY / PR", max(8, w/4)}, {"PROGRESS", 17}, {"STATUS", 10}}
	if w < 70 {
		return []column{{"REPO / PR", max(8, w-36)}, {"PROGRESS", 17}, {"STATUS", 8}}
	}
	if w >= 85 {
		cols = append(cols, column{"BUILD", 8})
	}
	if m.showCI() && w >= 110 {
		cols = append(cols, column{"CI", 14})
	}
	if w >= 140 {
		cols = append(cols, column{"BRANCH", max(18, w/7)})
	}
	if w >= 190 {
		cols = append(cols, column{"TITLE", w / 5})
	}
	used := 6
	for _, col := range cols {
		used += col.width + 2
	}
	if w-used >= 12 {
		cols = append(cols, column{"PHASE / WARNING", w - used})
	} else {
		cols[0].width += max(0, w-used+2)
	}
	return cols
}
func (m *Model) tableRow(p core.PR, selected bool, cols []column) string {
	build, phase := "—", "Waiting for checks"
	if j := primaryJob(p); j != nil {
		build, phase = value(j.Number), j.Phase
	}
	estimated := false
	for _, j := range p.Jobs {
		estimated = estimated || j.Estimated
	}
	status := p.Status()
	if p.State == "MERGED" || p.State == "CLOSED" {
		status = strings.ToLower(p.State)
	}
	color := muted
	switch status {
	case "passed", "merged":
		color = good
	case "failed", "stale":
		color = bad
	case "building":
		color = warn
	}
	if !p.Fresh && p.Error == "" {
		status = "loading"
	}
	marker, fold, issue := "  ", "▸ ", "  "
	if selected {
		marker = "› "
	}
	if m.expanded[p.Ref.URL] {
		fold = "▾ "
	}
	if warnings := p.Warnings(); len(warnings) > 0 {
		issue = m.paint(warn, "⚠ ")
		phase = warnings[0]
	}
	line := marker + issue + fold
	for i, col := range cols {
		if i > 0 {
			line += "  "
		}
		v := ""
		switch col.name {
		case "REPOSITORY / PR", "REPO / PR":
			v = fmt.Sprintf("%s #%d", p.Ref.Repo, p.Ref.Number)
		case "PROGRESS":
			v = m.bar(p, estimated, time.Now())
		case "STATUS":
			v = m.paint(color, status)
		case "BUILD":
			v = build
		case "CI":
			v = providers(p)
		case "BRANCH":
			v = value(p.Details.Branch)
		case "TITLE":
			v = p.Title
		case "PHASE / WARNING":
			v = core.Clean(phase)
		}
		line += cell(v, col.width)
	}
	line = cell(line, max(1, m.width))
	if selected && !m.noColor() {
		line = lipgloss.NewStyle().Reverse(true).Render(line)
	}
	return line
}
func value(s string) string {
	if s == "" {
		return "—"
	}
	return core.Clean(s)
}
func stamp(t time.Time) string {
	if t.IsZero() {
		return "—"
	}
	return t.Local().Format("2006-01-02 15:04:05 MST")
}

func (m *Model) details(p core.PR, selected bool) []string {
	var lines []string
	add := func(text string) {
		// Hard wrapping also preserves every character of long build URLs.
		for _, part := range strings.Split(ansi.Hardwrap(core.Clean(text), max(1, m.width-8), true), "\n") {
			lines = append(lines, "      │ "+part)
		}
	}
	d := p.Details
	add(p.Title)
	add(fmt.Sprintf("Branch: %s → %s  ·  Author: %s  ·  Draft: %t", value(d.Branch), value(d.BaseBranch), value(d.Author), d.Draft))
	add(fmt.Sprintf("Changes: +%d −%d · %d files · %d commits · %d comments", d.Additions, d.Deletions, d.ChangedFiles, d.Commits, d.Comments))
	add(fmt.Sprintf("Review: %s · Mergeable: %s · State: %s", value(d.ReviewDecision), value(d.Mergeable), p.FinalStatus()))
	add("Created: " + stamp(d.CreatedAt) + " · Updated: " + stamp(d.UpdatedAt))
	add("Head: " + value(p.Head) + " · Last refresh: " + stamp(p.LastSuccess))
	add("PR URL: " + p.Ref.URL)
	for _, issue := range p.Warnings() {
		add("⚠ " + issue)
	}
	if len(p.Jobs) == 0 {
		add("No CI checks reported yet.")
		return lines
	}
	jobIndex := 0
	if selected {
		m.selectedJob(p)
		jobIndex = m.jobCursor
	}
	job := p.Jobs[jobIndex]
	passed, finished := 0, 0
	for _, j := range p.Jobs {
		if core.Terminal(j.Status) {
			finished++
		}
		if core.Passing(j.Status) {
			passed++
		}
	}
	add(fmt.Sprintf("Checks: %d passing · %d finished / %d total · [Tab/Shift+Tab] choose check", passed, finished, len(p.Jobs)))
	add(fmt.Sprintf("Check %d/%d: %s · %s · #%s · %s", jobIndex+1, len(p.Jobs), job.Provider, job.Name, value(job.Number), job.Status))
	elapsed := job.Duration
	if !core.Terminal(job.Status) && !job.StartedAt.IsZero() {
		elapsed = time.Since(job.StartedAt)
	}
	expected := "—"
	if job.ExpectedDuration > 0 {
		expected = core.Duration(job.ExpectedDuration)
	}
	add(fmt.Sprintf("Phase: %s · Elapsed: %s · Expected: %s", job.Phase, core.Duration(elapsed), expected))
	add("CI URL: " + value(job.URL))
	return lines
}

func (m *Model) View() (view string) {
	if m.noColor() {
		defer func() { view = ansi.Strip(view) }()
	}
	if m.help {
		return m.helpView()
	}
	width := max(1, m.width)
	var lines []string
	add := func(s string) { lines = append(lines, ansi.Truncate(s, width, "…")) }
	state := "●"
	if m.busy {
		state = "◐"
	}
	if m.paused {
		state = "‖ paused"
	}
	demo := ""
	if m.Demo {
		demo = " · DEMO"
	}
	dir := "↑"
	if m.Config.Descending {
		dir = "↓"
	}
	add(m.paint(accent, state+" gprm") + m.paint(muted, fmt.Sprintf("%s  ·  every %s  ·  sort %s%s  ·  quit %s", demo, m.Config.Interval, m.Config.Sort, dir, m.Config.AutoQuit)))
	count, passed, running, warnings := 0, 0, 0, 0
	for _, p := range m.PRs {
		if p.Removed {
			continue
		}
		count++
		if p.Status() == "passed" {
			passed++
		}
		if p.Status() == "building" {
			running++
		}
		if len(p.Warnings()) > 0 {
			warnings++
		}
	}
	filter := ""
	if m.filter != "" {
		filter = fmt.Sprintf(" · filter %q", core.Clean(m.filter))
	}
	add(m.paint(muted, fmt.Sprintf("%d PRs  ·  %d building  ·  %d passing  ·  %d warnings%s", count, running, passed, warnings, filter)))
	add("")
	cols := m.columns()
	header := "      "
	for i, col := range cols {
		if i > 0 {
			header += "  "
		}
		header += cell(col.name, col.width)
	}
	add(m.paint(muted, header))
	rows := core.Sorted(m.PRs, m.filter, m.Config.Sort, m.Config.Descending)
	m.cursor = max(0, min(m.cursor, len(rows)-1))
	var body []string
	focus := 0
	for pos, i := range rows {
		p := m.PRs[i]
		selected := pos == m.cursor
		if selected {
			focus = len(body)
		}
		body = append(body, m.tableRow(p, selected, cols))
		if m.expanded[p.Ref.URL] {
			detail := m.details(p, selected)
			if selected {
				m.detailScroll = min(m.detailScroll, len(detail))
				focus += m.detailScroll
			}
			body = append(body, detail...)
		}
	}
	if len(body) == 0 {
		body = []string{"  Paste PRs with [v], add with [a], or discover with [d]."}
		if m.filter != "" {
			body[0] = "  No matches. [Esc] clears the filter."
		}
	}
	height := m.visible()
	m.scroll = max(0, min(m.scroll, max(0, len(body)-height)))
	if focus < m.scroll {
		m.scroll = focus
	}
	if focus >= m.scroll+height {
		m.scroll = focus - height + 1
	}
	for n := 0; n < height; n++ {
		if i := m.scroll + n; i < len(body) {
			add(body[i])
		} else {
			add("")
		}
	}
	add(m.paint(muted, strings.Repeat("─", width)))
	if i := m.selected(); i >= 0 {
		p := m.PRs[i]
		label := fmt.Sprintf("%s #%d · %s", p.Ref.Repo, p.Ref.Number, p.Title)
		if job := m.selectedJob(p); job != nil {
			label = fmt.Sprintf("Check %d/%d · %s · %s · %s", m.jobCursor+1, len(p.Jobs), job.Provider, job.Name, job.Phase)
		}
		add(m.paint(accent, core.Clean(label)))
		if issues := p.Warnings(); len(issues) > 0 {
			add(m.paint(warn, "⚠ "+core.Clean(strings.Join(issues, " · "))))
		} else {
			add(m.paint(muted, "[Enter] Expand/collapse PR details and URLs · [y] Copy this PR · [Y] Copy filtered table"))
		}
	} else {
		add("Clipboard accepts several PR links, including prose.")
		add("~ estimated time · ! overdue · — unknown progress")
	}
	add(m.paint(warn, core.Clean(m.notice)))
	for _, line := range m.footer() {
		add(m.paint(muted, line))
	}
	if m.height > 0 && len(lines) > m.height {
		lines = append(lines[:max(0, m.height-1)], lines[len(lines)-1])
	}
	return strings.Join(lines, "\n")
}

func (m *Model) helpView() string {
	text := `gprm · keys                                         [? / Esc] Close

Add       [v / Ctrl+V] Clipboard   [a] Add PRs   [d] Discover
Navigate  [↑/k ↓/j] Select PR      [PgUp/PgDn] Page   [g/G] First/last
Details   [Enter / Space] Toggle  [→/←] Expand/collapse
          [Tab / Shift+Tab] Next/previous CI check
          [ / ] Scroll through expanded details (including long URLs)
Open      [o] GitHub PR           [b] Selected CI URL
Copy      [y] Selected PR JSON    [Y] Filtered table JSON
          [c] gh request command [C] Jenkins curl requests
Arrange   [/] Fuzzy filter        [Esc] Clear filter
          [s] Repo/progress sort  [r] Reverse sort
Watch     [p] Pause/resume        [R] Refresh now
          [x] Remove selected PR from monitoring
Quit      [q / Q / Ctrl+C] Quit and print summary

⚠ warns about unavailable CI URLs/details or a failed refresh.
Expand the row to read warnings and full URLs; [ / ] scroll details.
JSON contains the latest displayed data and freshness/error fields.
[Y] includes filtered rows outside the viewport, in table order.
~ estimated progress; ! overdue; — unknown. Red → green → orange/red.
--no-color (or NO_COLOR) disables styling.
Auto-quit considers all monitored PRs, including filtered-out rows.`
	var lines []string
	for _, line := range strings.Split(text, "\n") {
		lines = append(lines, ansi.Truncate(line, max(1, m.width), "…"))
	}
	if m.height > 0 && len(lines) > m.height {
		lines = lines[:m.height]
	}
	return strings.Join(lines, "\n")
}
