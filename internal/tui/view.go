package tui

import (
	"fmt"
	"os"
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

func paint(style lipgloss.Style, s string) string {
	if _, ok := os.LookupEnv("NO_COLOR"); ok {
		return s
	}
	return style.Render(s)
}
func cell(s string, n int) string {
	s = ansi.Truncate(s, max(n, 1), "…")
	return s + strings.Repeat(" ", max(0, n-ansi.StringWidth(s)))
}
func bar(p float64, estimated bool) string {
	if p < 0 {
		return paint(muted, "░░░░░░░░░░  —   ")
	}
	n := max(0, min(10, int(p*10)))
	mark := " "
	if estimated && p < 1 {
		mark = "~"
	}
	return paint(good, strings.Repeat("█", n)) + paint(muted, strings.Repeat("░", 10-n)) + fmt.Sprintf(" %s%3.0f%%", mark, p*100)
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
func (m *Model) View() string {
	if m.help {
		return m.helpView()
	}
	width := max(20, m.width)
	lines := []string{}
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
	add(paint(accent, state+" gprm") + paint(muted, fmt.Sprintf("%s  ·  every %s  ·  sort %s%s  ·  quit %s", demo, m.Config.Interval, m.Config.Sort, dir, m.Config.AutoQuit)))
	count, passed, running, stale := 0, 0, 0, 0
	for _, p := range m.PRs {
		if p.Removed {
			continue
		}
		count++
		switch p.Status() {
		case "passed":
			passed++
		case "building":
			running++
		case "stale":
			stale++
		}
	}
	filter := ""
	if m.filter != "" {
		filter = fmt.Sprintf(" · filter %q", core.Clean(m.filter))
	}
	add(paint(muted, fmt.Sprintf("%d PRs  ·  %d building  ·  %d passing  ·  %d stale%s", count, running, passed, stale, filter)))
	add("")
	ci := m.showCI() && width >= 110
	repoWidth := max(18, min(34, width/4))
	phaseWidth := max(10, width-repoWidth-53)
	if ci {
		phaseWidth -= 17
	}
	header := cell("REPOSITORY / PR", repoWidth) + "  " + cell("PROGRESS", 17) + "  " + cell("BUILD", 10) + "  " + cell("STATUS", 14)
	if ci {
		header += "  " + cell("CI", 15)
	}
	if width >= 85 {
		header += "  PHASE"
	}
	add(paint(muted, header))
	rows := core.Sorted(m.PRs, m.filter, m.Config.Sort, m.Config.Descending)
	m.cursor = max(0, min(m.cursor, len(rows)-1))
	start := max(0, m.cursor-m.visible()+1)
	end := min(len(rows), start+m.visible())
	if len(rows) == 0 {
		if m.filter != "" {
			add("  No matches. Esc clears the filter.")
		} else {
			add("  Paste PRs with v, add with a, or discover your open PRs with d.")
		}
	}
	for pos := start; pos < end; pos++ {
		p := m.PRs[rows[pos]]
		build, phase := "—", "Waiting for checks"
		estimated := false
		if j := primaryJob(p); j != nil {
			build = j.Number
			if build == "" {
				build = "—"
			}
			phase = j.Phase
		}
		for _, j := range p.Jobs {
			if j.Estimated {
				estimated = true
			}
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
		line := cell(fmt.Sprintf("%s #%d", core.Clean(p.Ref.Repo), p.Ref.Number), repoWidth) + "  " + cell(bar(p.Progress(), estimated), 17) + "  " + cell(core.Clean(build), 10) + "  " + paint(color, cell(status, 14))
		if ci {
			line += "  " + cell(providers(p), 15)
		}
		if width >= 85 {
			line += "  " + cell(phase, phaseWidth)
		}
		line = ansi.Truncate(line, width, "…")
		if pos == m.cursor {
			line = lipgloss.NewStyle().Reverse(true).Render(line)
		}
		add(line)
	}
	used := end - start
	if len(rows) == 0 {
		used = 1
	}
	for i := used; i < m.visible(); i++ {
		add("")
	}
	add(paint(muted, strings.Repeat("─", width)))
	if i := m.selected(); i >= 0 {
		p := m.PRs[i]
		add(paint(accent, core.Clean(p.Title)) + paint(muted, " · "+p.FinalStatus()))
		if job := m.selectedJob(p); job != nil {
			label := fmt.Sprintf("[%d/%d] %s · %s · %s", m.jobCursor+1, len(p.Jobs), job.Provider, job.Name, job.Phase)
			if job.Number != "" {
				label += " · #" + job.Number
			}
			if !job.StartedAt.IsZero() {
				d := job.Duration
				if !core.Terminal(job.Status) {
					d = time.Since(job.StartedAt)
				}
				label += " · " + core.Duration(d)
			}
			add(core.Clean(label))
			detail := job.URL
			if job.Warning != "" {
				detail = job.Warning
			}
			if p.Error != "" {
				detail = p.Error
			}
			add(paint(muted, core.Clean(detail)))
		} else {
			add("Waiting for CI checks to be reported by GitHub.")
			add(paint(muted, core.Clean(p.Error)))
		}
	} else {
		add("Clipboard accepts several links, including links copied from a message.")
		add("Build progress: ~ estimated time · Actions: completed steps · — unknown")
		add("")
	}
	add(paint(warn, core.Clean(m.notice)))
	if m.mode != "" {
		add(paint(accent, m.mode+" › ") + m.input.View())
	} else {
		add(paint(muted, "v paste  a add  d discover  / filter  s sort  r reverse  o PR  b build  ? help  Q quit"))
	}
	// Very short terminals still get the input and key hints.
	if len(lines) > m.height && m.height > 0 {
		lines = append(lines[:max(0, m.height-1)], lines[len(lines)-1])
	}
	return strings.Join(lines, "\n")
}
func (m *Model) helpView() string {
	text := `gprm · keys

Add PRs       v / Ctrl+V  import clipboard links
              a          type or paste multiple PR references
              d          discover your open PRs
Navigate      ↑/k ↓/j    select PR
              PgUp/PgDn  page through PRs
              g / G      first / last PR
              Tab / ↵    next check; Shift+Tab previous check
Arrange       /          fuzzy filter; Esc clears
              s          sort by repository / progress
              r          reverse sort
Watch         p          pause / resume polling
              R          refresh now
              x          remove selected PR from monitoring
Open          o          selected PR in browser
              b          selected check's build in browser
Other         ? / Esc    close help
              Q / Ctrl+C quit and print summary

~ progress is a time estimate; — means unknown.
Other CI checks use status from GitHub. No checks never means passing.
Auto-quit considers all monitored PRs, including filtered-out rows.
Startup and auto-quit defaults live in config.toml; flags override them.`
	var lines []string
	for _, line := range strings.Split(text, "\n") {
		lines = append(lines, ansi.Truncate(line, max(20, m.width), "…"))
	}
	if m.height > 0 && len(lines) > m.height {
		lines = lines[:m.height]
	}
	return strings.Join(lines, "\n")
}
