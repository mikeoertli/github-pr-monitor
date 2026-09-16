package tui

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/mikeoertli/github-pr-monitor/internal/core"
	"github.com/muesli/termenv"
)

func TestProgressColorsAndOverdue(t *testing.T) {
	now := time.Now()
	for _, tc := range []struct {
		name            string
		progress, ratio float64
		status, tone    string
		overdue         bool
	}{
		{"empty", 0, 0, "running", "red", false},
		{"early", .24, 0, "running", "red", false},
		{"quarter", .25, 0, "running", "orange", false},
		{"half", .5, 0, "running", "yellow", false},
		{"nearly done", .9, .9, "running", "green", false},
		{"at estimate", .99, 1, "running", "green", false},
		{"over estimate", .99, 1.1, "running", "orange", true},
		{"well overdue", .99, 1.25, "running", "red", true},
		{"passed after overrun", 1, 1.5, "passed", "green", false},
		{"failed", 1, .8, "failed", "red", false},
		{"unknown", -1, 0, "queued", "muted", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			job := core.Job{Status: tc.status, Progress: tc.progress}
			if tc.ratio > 0 {
				job.Estimated = true
				job.ExpectedDuration = 10 * time.Minute
				job.StartedAt = now.Add(-time.Duration(tc.ratio * float64(job.ExpectedDuration)))
			}
			p := core.PR{Fresh: true, Jobs: []core.Job{job}}
			tone, overdue := progressColor(p, now)
			if tone != tc.tone || overdue != tc.overdue {
				t.Fatalf("got %s/%v, want %s/%v", tone, overdue, tc.tone, tc.overdue)
			}
			m := demoModel()
			m.Config.NoColor = true
			bar := m.bar(p, job.Estimated, now)
			if strings.Contains(bar, "!") != tc.overdue {
				t.Fatalf("overdue marker: %q", bar)
			}
			if strings.Contains(bar, "\x1b") {
				t.Fatal("uncolored bar contains escapes")
			}
		})
	}
	// One overdue check should remain visible even when other checks are unknown.
	p := core.PR{Fresh: true, Jobs: []core.Job{{Status: "running", Progress: -1}, {Status: "running", Progress: .99, Estimated: true, ExpectedDuration: time.Minute, StartedAt: now.Add(-2 * time.Minute)}}}
	if tone, late := progressColor(p, now); tone != "red" || !late {
		t.Fatal(tone, late)
	}
	p.Jobs[1].Stale = true
	if tone, late := progressColor(p, now); tone != "muted" || late {
		t.Fatal("stale timing treated as fresh")
	}
}

func TestNoColorRemovesAllStyling(t *testing.T) {
	old := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(old) })
	t.Setenv("TERM", "xterm-256color")
	t.Setenv("NO_COLOR", "")
	os.Unsetenv("NO_COLOR")
	m := demoModel()
	if !strings.Contains(m.View(), "\x1b") {
		t.Fatal("expected styles with color enabled")
	}
	m.Config.NoColor = true
	for _, mode := range []string{"", "add", "filter"} {
		if mode != "" {
			m.startInput(mode)
		}
		if view := m.View(); strings.Contains(view, "\x1b") {
			t.Fatalf("styles remain in %s view", mode)
		}
	}
	m.mode = ""
	if !strings.Contains(m.View(), "› ") {
		t.Fatal("plain selected-row marker missing")
	}
	m.Config.NoColor = false
	t.Setenv("NO_COLOR", "1")
	if strings.Contains(m.View(), "\x1b") {
		t.Fatal("NO_COLOR ignored")
	}
}
