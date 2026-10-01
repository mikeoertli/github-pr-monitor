package tui

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
)

func TestStyledDetailsWrapAndRespectNoColor(t *testing.T) {
	old := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(old) })
	t.Setenv("TERM", "xterm-256color")
	t.Setenv("NO_COLOR", "")
	os.Unsetenv("NO_COLOR")
	m := demoModel()
	p := m.PRs[2]
	p.StatusChangedAt = time.Time{}
	p.Jobs[0].CompletedAt = time.Time{}
	p.Title = "PR title with untrusted escape \x1b[2J"
	p.Jobs[0].Warning = "Warning with escape \x1b]52;c;ignored\x07"
	p.Jobs[0].URL = "https://ci.example.com/" + strings.Repeat("long-segment/", 20) + "last-part"
	for _, width := range []int{200, 60, 20} {
		m.width = width
		m.Config.NoColor = false
		colored := m.details(p, false)
		styled := strings.Join(colored, "\n")
		if !strings.Contains(styled, "\x1b[") {
			t.Fatal("details lack styling")
		}
		if strings.Contains(styled, "\x1b[2J") || strings.Contains(styled, "\x1b]52") {
			t.Fatal("provider escapes reached terminal")
		}
		m.Config.NoColor = true
		plain := strings.Join(m.details(p, false), "\n")
		if strings.Contains(plain, "\x1b") || ansi.Strip(styled) != plain {
			t.Fatal("color changed content or wrapping")
		}
		var unwrapped strings.Builder
		for _, line := range colored {
			if ansi.StringWidth(line) > width {
				t.Fatalf("width %d overflow: %q", width, line)
			}
			unwrapped.WriteString(strings.TrimPrefix(ansi.Strip(line), "      │ "))
		}
		if !strings.Contains(unwrapped.String(), p.Jobs[0].URL) {
			t.Fatal("wrapped URL lost characters")
		}
		for _, heading := range []string{"PR OVERVIEW", "ACTIVITY", "CI CHECKS"} {
			if !strings.Contains(unwrapped.String(), heading) {
				t.Fatal("missing section", heading)
			}
		}
	}
}
