package tui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mikeoertli/github-pr-monitor/internal/core"
)

type branchUpdateMsg struct {
	PR  core.PR
	Err error
}

func (m *Model) jiraID(p core.PR) string { return core.JiraID(p.Title, m.Config.Jira.ProjectPrefixes) }
func (m *Model) showJira() bool {
	for _, p := range m.scopedPRs() {
		if m.jiraID(p) != "" {
			return true
		}
	}
	return false
}
func (m *Model) jiraAction(copyURL bool) tea.Cmd {
	i := m.selected()
	if i < 0 {
		m.notice = "No PR selected."
		return nil
	}
	id := m.jiraID(m.PRs[i])
	if id == "" {
		m.notice = "No configured Jira ticket prefix at the start of this PR title."
		return nil
	}
	raw := m.Config.Jira.URL(id)
	if raw == "" {
		m.notice = "Set [jira] base_url in gprm_config.toml to open or copy Jira links."
		return nil
	}
	ctx := m.Context
	if copyURL {
		copy := m.Actions.Copy
		if copy == nil {
			m.notice = "Clipboard copying is unavailable."
			return nil
		}
		return func() tea.Msg { return copyMsg{Label: "Jira URL", Err: copy(ctx, raw)} }
	}
	open := m.Actions.Open
	if open == nil {
		m.notice = "Browser opening is unavailable."
		return nil
	}
	return func() tea.Msg { return openMsg{Err: open(ctx, raw)} }
}

func (m *Model) startBranchUpdate() {
	if m.Demo {
		m.notice = "Branch updates are disabled in the offline demo."
		return
	}
	if m.updating {
		m.notice = "A branch update is already in progress."
		return
	}
	i := m.selected()
	if i < 0 {
		m.notice = "No PR selected."
		return
	}
	p := m.PRs[i]
	if _, pending := m.pendingUpdates[p.Ref.URL]; pending {
		m.notice = "Waiting for GitHub to report this branch update."
		return
	}
	if !p.CanUpdateBranch() {
		m.notice = "Branch update unavailable: refresh first; GitHub must allow an update for your account."
		return
	}
	if m.Actions.UpdateBranch == nil {
		m.notice = "Branch updates are unavailable."
		return
	}
	m.updatePR = p
	m.mode = "update"
	m.notice = "This merges the target into the PR branch and may restart CI."
}

func (m *Model) confirmBranchUpdate() tea.Cmd {
	p := m.updatePR
	m.mode = ""
	valid := false
	for _, current := range m.PRs {
		if current.Ref.URL == p.Ref.URL && !current.Removed && current.CanUpdateBranch() && current.Head == p.Head && current.Details.BaseBranch == p.Details.BaseBranch {
			valid = true
			break
		}
	}
	if !valid {
		m.notice = "PR changed; refresh and choose Update branch again."
		return nil
	}
	m.updating = true
	m.notice = fmt.Sprintf("Requesting branch update for %s #%d…", p.Ref.Repo, p.Ref.Number)
	ctx, update := m.Context, m.Actions.UpdateBranch
	return func() tea.Msg { return branchUpdateMsg{PR: p, Err: update(ctx, p)} }
}
