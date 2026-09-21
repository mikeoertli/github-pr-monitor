package tui

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/mikeoertli/github-pr-monitor/internal/core"
)

var orange = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Dark: "#fb923c", Light: "#c2410c"})

func (m *Model) noColor() bool {
	_, noColor := os.LookupEnv("NO_COLOR")
	return m.Config.NoColor || noColor || strings.EqualFold(os.Getenv("TERM"), "dumb")
}

// Keep runtime ratios separate from the capped percentage: a running build at
// 130% of its estimated duration must not look like a completed build.
func progressColor(p core.PR, now time.Time) (string, bool) {
	if p.Status() == "stale" || !p.Fresh {
		return "muted", false
	}
	ratio := 0.0
	failed := false
	for _, j := range p.Jobs {
		if core.Terminal(j.Status) {
			if !core.Passing(j.Status) {
				failed = true
			}
			continue
		}
		if j.Estimated && j.ExpectedDuration > 0 && !j.StartedAt.IsZero() {
			ratio = max(ratio, float64(now.Sub(j.StartedAt))/float64(j.ExpectedDuration))
		}
	}
	if ratio > 1 {
		if ratio >= 1.25 {
			return "red", true
		}
		return "orange", true
	}
	if failed {
		return "red", false
	}
	switch p.Progress() {
	case -1:
		return "muted", false
	}
	switch {
	case p.Progress() < .25:
		return "red", false
	case p.Progress() < .5:
		return "orange", false
	case p.Progress() < .75:
		return "yellow", false
	default:
		return "green", false
	}
}

func (m *Model) bar(p core.PR, estimated bool, now time.Time) string {
	if len(p.Jobs) == 0 {
		return m.paint(muted, "—")
	}
	tone, overdue := progressColor(p, now)
	style := map[string]lipgloss.Style{"red": bad, "orange": orange, "yellow": warn, "green": good, "muted": muted}[tone]
	progress := p.Progress()
	if progress < 0 {
		label := "░░░░░░░░░░  —"
		if overdue {
			label += " !"
		}
		return m.paint(style, label)
	}
	n := max(0, min(10, int(progress*10)))
	mark := " "
	if estimated && progress < 1 {
		mark = "~"
	}
	label := fmt.Sprintf(" %s%3.0f%%", mark, progress*100)
	if overdue {
		label += "!"
	}
	// Color the unfilled section too so a zero-progress build visibly starts red.
	return m.paint(style, strings.Repeat("█", n)+strings.Repeat("░", 10-n)+label)
}
