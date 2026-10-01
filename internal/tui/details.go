package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/mikeoertli/github-pr-monitor/internal/core"
)

var detailTitle = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Dark: "#f8fafc", Light: "#111827"}).Bold(true)
var detailLabel = muted.Bold(true)
var detailSection = accent.Underline(true)
var detailLink = accent.Bold(false).Underline(true)

func detailStatusStyle(status string) lipgloss.Style {
	switch strings.ToLower(status) {
	case "passed", "merged", "manually merged", "mergeable", "approved", "clean", "success":
		return good.Bold(true)
	case "failed", "failure", "conflicts", "conflicting", "dirty", "changes_requested", "error":
		return bad.Bold(true)
	case "building", "running", "pending", "queued", "blocked", "behind", "draft", "stale", "review_required", "unstable":
		return warn.Bold(true)
	default:
		return detailTitle
	}
}

func (m *Model) details(p core.PR, selected bool) []string {
	var lines []string
	// Clean provider text before adding our styles. ANSI-aware wrapping keeps
	// long URLs intact and does not count formatting bytes toward line widths.
	styled := func(style lipgloss.Style, text string) string { return m.paint(style, core.Clean(text)) }
	label := func(name string) string { return m.paint(detailLabel, name+": ") }
	separator := m.paint(muted, " · ")
	add := func(text string) {
		for _, part := range strings.Split(ansi.Hardwrap(text, max(1, m.width-8), true), "\n") {
			lines = append(lines, m.paint(muted, "      │ ")+part)
		}
	}
	section := func(name string) { add(m.paint(detailSection, name)) }
	field := func(name, text string) string { return label(name) + value(text) }
	state := func(name, text string) string { return label(name) + styled(detailStatusStyle(text), value(text)) }
	d := p.Details
	add(styled(detailTitle, p.Title))
	if !p.StatusChangedAt.IsZero() {
		basis := "provider event"
		if p.StatusTimeObserved {
			basis = "first observed; exact event time unavailable"
		}
		add(label("Status") + styled(detailStatusStyle(p.StatusChangedStatus), value(p.StatusChangedStatus)+" "+p.StatusAge(time.Now())) + separator + styled(muted, stamp(p.StatusChangedAt)+" ("+basis+")"))
	}
	if p.Closed() {
		if retention := m.Config.RetentionDuration(); retention < 0 {
			add(styled(muted, "Kept until dismissed · [x] Dismiss"))
		} else if at := p.CompletionTime(); !at.IsZero() {
			add(label("Kept until") + styled(muted, stamp(at.Add(retention))+" · [x] Dismiss sooner"))
		}
	}
	section("PR OVERVIEW")
	add(label("Branch") + value(d.Branch) + m.paint(muted, " → ") + styled(accent, value(d.BaseBranch)) + separator + field("Author", d.Author) + separator + state("Draft", fmt.Sprint(d.Draft)))
	add(label("Changes") + styled(good, fmt.Sprintf("+%d", d.Additions)) + " " + styled(bad, fmt.Sprintf("−%d", d.Deletions)) + separator + fmt.Sprintf("%d files · %d commits · %d comments", d.ChangedFiles, d.Commits, d.Comments))
	mergeable := value(d.Mergeable)
	if p.State == "MERGED" || p.State == "CLOSED" {
		mergeable = "n/a (" + strings.ToLower(p.State) + ")"
	}
	add(state("Review", value(d.ReviewDecision)) + separator + state("Mergeable", mergeable) + separator + state("State", p.FinalStatus()))
	add(state("Merge state", value(d.MergeStateStatus)))
	if p.CanUpdateBranch() {
		add(styled(warn, "↑ Update branch available · [u] Confirm update · [U] Update without confirmation"))
	}
	if _, pending := m.pendingUpdates[p.Ref.URL]; pending {
		add(styled(warn, "↑ Branch update requested · waiting for GitHub"))
	}
	if id := m.jiraID(p); id != "" {
		link := m.Config.Jira.URL(id)
		if link == "" {
			link = m.paint(muted, "configure [jira] base_url for links")
		} else {
			link = styled(detailLink, link)
		}
		add(label("Jira") + styled(accent, id) + separator + link)
	}
	add(label("PR URL") + styled(detailLink, value(p.Ref.URL)))
	section("ACTIVITY")
	add(label("Created") + styled(muted, stamp(d.CreatedAt)) + separator + label("GitHub data updated") + styled(muted, stamp(d.UpdatedAt)))
	if !d.MergedAt.IsZero() {
		add(label("Merged") + styled(good, stamp(d.MergedAt)))
	} else if !d.ClosedAt.IsZero() {
		add(field("Closed", stamp(d.ClosedAt)))
	}
	add(label("Head") + styled(muted, value(p.Head)))
	add(label("Last successful data fetch") + styled(muted, stamp(p.LastSuccess)))
	if p.Error != "" {
		add(styled(warn, "Last attempt (failed): "+stamp(p.LastAttempt)+" · Showing previous data"))
	} else if _, pending := m.pendingUpdates[p.Ref.URL]; pending {
		add(styled(warn, "Showing previous head while the branch update is pending"))
	} else if !p.Fresh {
		add(styled(warn, "Saved snapshot · awaiting a successful refresh"))
	}
	section("CI CHECKS")
	for _, issue := range p.Warnings() {
		add(styled(warn, "⚠ "+issue))
	}
	if len(p.Jobs) == 0 {
		status, readiness := p.MergeReadiness()
		add(styled(detailStatusStyle(status), "No CI checks · "+readiness))
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
	add(label("Checks") + fmt.Sprintf("%d passing · %d finished / %d total", passed, finished, len(p.Jobs)) + separator + m.paint(muted, "[Tab/Shift+Tab] choose check"))
	jobStatus := job.Status
	event := job.StartedAt
	if core.Terminal(job.Status) {
		event = job.CompletedAt
	}
	if age := core.RelativeTime(event, time.Now()); age != "" {
		jobStatus += " " + age
	}
	add(label(fmt.Sprintf("Check %d/%d", jobIndex+1, len(p.Jobs))) + value(job.Provider) + separator + styled(detailTitle, value(job.Name)) + separator + "#" + value(job.Number) + separator + styled(detailStatusStyle(job.Status), jobStatus))
	elapsed := job.Duration
	if !core.Terminal(job.Status) && !job.StartedAt.IsZero() {
		elapsed = time.Since(job.StartedAt)
	}
	expected := "—"
	if job.ExpectedDuration > 0 {
		expected = core.Duration(job.ExpectedDuration)
	}
	add(field("Phase", job.Phase) + separator + field("Elapsed", core.Duration(elapsed)) + separator + field("Expected", expected))
	add(label("CI URL") + styled(detailLink, value(job.URL)))
	return lines
}
