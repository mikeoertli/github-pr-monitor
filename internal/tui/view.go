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
	for _, p := range m.scopedPRs() {
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

func (m *Model) shortcut(hint string) string {
	key, label, ok := strings.Cut(hint, "]")
	if !ok {
		return hint
	}
	if m.menuPressed(strings.TrimPrefix(key, "["), time.Now()) {
		return m.paint(menuPressedStyle, hint)
	}
	return m.paint(accent, key+"]") + label
}

// footer separates actions into labeled groups and keeps keycaps readable without color.
func (m *Model) footer() []string {
	if m.mode == "update" {
		p := m.updatePR
		prompt := fmt.Sprintf("UPDATE %s #%d: merge %s into %s?", p.Ref.Repo, p.Ref.Number, value(p.Details.BaseBranch), value(p.Details.Branch))
		lines := strings.Split(ansi.Hardwrap(prompt, max(1, m.width), true), "\n")
		for i := range lines {
			lines[i] = m.paint(warn, lines[i])
		}
		return append(lines, m.shortcut("[Enter] Update branch")+" · "+m.shortcut("[Esc] Cancel"))
	}
	if m.mode != "" {
		hint := "[a] Add"
		if m.mode == "filter" {
			hint = "[/] Filter"
		}
		return []string{m.shortcut(hint) + " › " + m.input.View(), m.shortcut("[Enter] Apply") + "   ·   " + m.shortcut("[Esc] Cancel")}
	}
	if m.width < 80 {
		open := m.paint(accent, "BROWSE  ") + m.shortcut("[g] Open PR") + " · " + m.shortcut("[c] Open CI")
		if m.showJira() {
			open += " · " + m.shortcut("[J] Open Jira")
		}
		copy := m.paint(accent, "COPY    ") + m.shortcut("[G] gh command") + " · " + m.shortcut("[C] Jenkins curl")
		if m.detailFocus != "" {
			return []string{open, copy, m.shortcut("[↑↓] Scroll") + " · " + m.shortcut("[←/Esc] Back"), m.shortcut("[u] Update branch") + " · " + m.shortcut("[U] Update now"), m.shortcut("[?] Keys") + " · " + m.shortcut("[q] Quit")}
		}
		return []string{open, copy, m.shortcut("[→] Read details"), m.shortcut("[u] Update branch") + " · " + m.shortcut("[U] Update now"), m.shortcut("[?] Keys") + " · " + m.shortcut("[q] Quit")}
	}
	groups := [][]string{
		{"BROWSE", "[g] Open PR", "[c] Open CI"},
		{"COPY", "[G] gh command", "[C] Jenkins curl"},
		{"PRS", "[d] Discover", "[/] Filter", "[s] Sort", "[r] Reverse", "[x] Dismiss", "[a] Add"},
		{"INSPECT", "[Enter] Details", "[→] Read details", "[←/Esc] Back", "[Tab] Check"},
		{"WATCH", "[↑↓] Select", "[p] Pause", "[R] Refresh", "[u] Update branch", "[U] Update now", "[?] Help", "[q] Quit"},
	}
	if m.detailFocus != "" {
		groups[4][1] = "[↑↓/PgUp/PgDn] Scroll details"
	}
	if m.showJira() {
		groups[0] = append(groups[0], "[J] Open Jira")
		groups[1] = append(groups[1], "[K] Copy Jira URL")
	}
	var lines []string
	for _, group := range groups {
		line := m.paint(accent, cell(group[0], 8))
		items := 0
		for _, hint := range group[1:] {
			if items > 0 && ansi.StringWidth(line)+ansi.StringWidth(hint)+5 > m.width {
				lines = append(lines, line)
				line = strings.Repeat(" ", 8)
				items = 0
			}
			if items > 0 {
				line += m.paint(muted, "  │  ")
			}
			line += m.shortcut(hint)
			items++
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
	if w < 70 {
		return []column{{"REPO / PR", max(8, w-36)}, {"PROGRESS", 17}, {"STATUS", 8}}
	}
	cols := []column{{"REPOSITORY / PR", max(18, w/6)}, {"PROGRESS", 17}, {"STATUS", 10}}
	if w >= 100 {
		cols[2].width = 22
	}
	jiraWidth := 12
	if w >= 80 && w < 100 && m.showJira() {
		cols[0].width = 14
		cols[2].width = 9
		jiraWidth = 10
	}
	used := 6
	for _, col := range cols {
		used += col.width + 2
	}
	add := func(name string, width, reserve int) {
		if used+width+2+reserve <= w {
			cols = append(cols, column{name, width})
			used += width + 2
		}
	}
	add("TARGET", max(10, min(20, w/10)), 0)
	if m.showJira() {
		add("JIRA", jiraWidth, 0)
	}
	if w >= 100 {
		add("BUILD", 7, 12)
	}
	if m.showCI() && w >= 110 {
		add("CI", 12, 12)
	}
	if w >= 140 {
		add("BRANCH", max(14, w/10), 12)
	}
	if w >= 190 {
		add("TITLE", max(16, w/10), 12)
	}
	if w-used >= 12 {
		cols = append(cols, column{"PHASE / WARNING", w - used})
	} else {
		cols[0].width += max(0, w-used+2)
	}
	return cols
}
func (m *Model) tableRow(p core.PR, selected bool, cols []column) string {
	build, phase := "—", "Loading PR status"
	if p.Fresh && len(p.Jobs) == 0 {
		_, readiness := p.MergeReadiness()
		phase = "No CI checks · " + readiness
	} else if p.Error != "" {
		phase = "PR status unavailable"
	}
	if j := primaryJob(p); j != nil {
		build, phase = value(j.Number), j.Phase
	}
	estimated := false
	for _, j := range p.Jobs {
		estimated = estimated || j.Estimated
	}
	status := p.DisplayStatus()
	color := muted
	switch status {
	case "passed", "merged", "mergeable":
		color = good
	case "failed", "stale", "conflicts":
		color = bad
	case "building", "blocked", "behind", "draft":
		color = warn
	}
	if !p.Fresh && p.Error == "" {
		status = "loading"
	}
	if _, pending := m.pendingUpdates[p.Ref.URL]; pending {
		status = "updating"
		color = accent
	}
	marker, fold, issue := "  ", "▸ ", "  "
	if selected {
		marker = "› "
	}
	if m.expanded[p.Ref.URL] {
		fold = "▾ "
	}
	if p.CanUpdateBranch() {
		issue = m.paint(accent, "↑ ")
		phase = "↑ Update available · " + phase
	}
	if _, pending := m.pendingUpdates[p.Ref.URL]; pending {
		issue = m.paint(accent, "↑ ")
		phase = "Branch update pending"
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
			label := status
			if col.width >= 20 && p.Fresh && p.Error == "" && status != "updating" && status != "stale" {
				if age := p.StatusAge(time.Now()); age != "" {
					label += " " + age
				}
			}
			v = m.paint(color, label)
		case "BUILD":
			v = build
		case "CI":
			v = providers(p)
		case "TARGET":
			v = value(m.targetBranchLabel(p.Details.BaseBranch))
		case "JIRA":
			v = value(m.jiraID(p))
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

// Shorten only the table label, never the branch used in actions or matching.
func (m *Model) targetBranchLabel(branch string) string {
	prefix := ""
	for _, candidate := range m.Config.TargetBranchIgnoredPrefixes {
		if len(candidate) > len(prefix) && strings.HasPrefix(branch, candidate) {
			prefix = candidate
		}
	}
	label := strings.TrimPrefix(branch, prefix)
	if label == "" {
		return branch
	}
	return label
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
	count, passed, running, warnings, merged, closed := 0, 0, 0, 0, 0, 0
	for _, p := range m.scopedPRs() {
		if p.Removed {
			continue
		}
		count++
		if p.State == "MERGED" {
			merged++
		} else if p.State == "CLOSED" {
			closed++
		}
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
	add(m.paint(muted, fmt.Sprintf("%d PRs  ·  %d building  ·  %d passing  ·  %d merged  ·  %d closed  ·  %d warnings%s", count, running, passed, merged, closed, warnings, filter)))
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
	m.selected() // Keep details attached to the same PR after refreshes and sorting.
	rows := core.Sorted(m.PRs, m.filter, m.Config.Sort, m.Config.Descending, m.Config.Jira.ProjectPrefixes...)
	m.cursor = max(0, min(m.cursor, len(rows)-1))
	var body []string
	focus, detailEnd := 0, 0
	previousGroup := "\x00"
	for pos, i := range rows {
		p := m.PRs[i]
		if core.GroupsByJira(m.Config.Sort) {
			group := m.jiraID(p)
			if group != previousGroup {
				label := group
				if label == "" {
					label = "No Jira ticket"
				}
				body = append(body, m.paint(accent, "      ── "+label+" ──"))
				previousGroup = group
			}
		}
		selected := pos == m.cursor
		if selected {
			focus = len(body)
		}
		body = append(body, m.tableRow(p, selected, cols))
		if m.expanded[p.Ref.URL] {
			detail := m.details(p, selected)
			body = append(body, m.paint(accent, fmt.Sprintf("      ├ DETAILS · %d lines · [→] Focus to scroll", len(detail))))
			body = append(body, detail...)
			if selected {
				detailEnd = len(body)
			}
		}
	}
	if len(body) == 0 {
		body = []string{"  Discover PRs with [d] or add with [a]."}
		if m.filter != "" {
			body[0] = "  No matches. [Esc] clears the filter."
		}
	}
	height := m.visible()
	if i := m.selected(); i >= 0 && m.detailFocus != "" {
		detail := m.details(m.PRs[i], true)
		m.detailScroll = max(0, min(m.detailScroll, len(detail)-m.detailHeight()))
		end := min(len(detail), m.detailScroll+m.detailHeight())
		above, below := "↑ top", "↓ end"
		if m.detailScroll > 0 {
			above = "↑ more"
		}
		if end < len(detail) {
			below = "↓ more"
		}
		body = []string{m.tableRow(m.PRs[i], true, cols), m.paint(accent, fmt.Sprintf("  DETAILS · %d–%d/%d · %s · %s · [←/Esc] Back", m.detailScroll+1, end, len(detail), above, below))}
		body = append(body, detail[m.detailScroll:end]...)
		// The table's scroll position is preserved for returning from details.
		for n := 0; n < height; n++ {
			if n < len(body) {
				add(body[n])
			} else {
				add("")
			}
		}
	} else {
		m.scroll = max(0, min(m.scroll, max(0, len(body)-height)))
		if focus < m.scroll {
			m.scroll = focus
		}
		if focus >= m.scroll+height {
			m.scroll = focus - height + 1
		}
		for n := 0; n < height; n++ {
			if n == height-1 && detailEnd > m.scroll+height && focus >= m.scroll {
				add(m.paint(accent, "      ↓ More details · [→] Focus to scroll"))
			} else if i := m.scroll + n; i < len(body) {
				add(body[i])
			} else {
				add("")
			}
		}
	}
	add(m.paint(muted, strings.Repeat("─", width)))
	if i := m.selected(); i >= 0 {
		p := m.PRs[i]
		label := fmt.Sprintf("%s #%d · %s", p.Ref.Repo, p.Ref.Number, p.Title)
		if job := m.selectedJob(p); job != nil {
			label = fmt.Sprintf("Check %d/%d · %s · %s · %s", m.jobCursor+1, len(p.Jobs), job.Provider, job.Name, job.Phase)
		}
		if p.Fresh && p.Error == "" && p.DisplayStatus() != "stale" {
			if age := p.StatusAge(time.Now()); age != "" {
				label += " · " + p.DisplayStatus() + " " + age
			}
		}
		add(m.paint(accent, core.Clean(label)))
		if issues := p.Warnings(); len(issues) > 0 {
			add(m.paint(warn, "⚠ "+core.Clean(strings.Join(issues, " · "))))
		} else if m.detailFocus != "" {
			add(m.paint(accent, "[↑↓] Scroll details · [PgUp/PgDn] Page · [←/Esc] Back to table"))
		} else {
			add(m.paint(muted, "[Enter] Expand/collapse · [→] Focus details to scroll"))
		}
	} else {
		add("Enter one or more PR links or owner/repo#123 references.")
		add("~ estimated time · ! overdue · — unknown progress")
	}
	add(m.paint(warn, core.Clean(m.notice)))
	for _, line := range m.footer() {
		add(line)
	}
	if m.height > 0 && len(lines) > m.height {
		lines = append(lines[:max(0, m.height-1)], lines[len(lines)-1])
	}
	return strings.Join(lines, "\n")
}
