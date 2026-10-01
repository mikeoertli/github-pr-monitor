package tui

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/mikeoertli/github-pr-monitor/internal/core"
	"github.com/mikeoertli/github-pr-monitor/internal/provider"
)

func (m *Model) copyCommand(jenkins bool) tea.Cmd {
	i := m.selected()
	if i < 0 {
		m.notice = "No PR selected."
		return nil
	}
	if m.Actions.Copy == nil {
		m.notice = "Clipboard copying is unavailable."
		return nil
	}
	p := m.PRs[i]
	label := "gh pr view command"
	var command string
	if jenkins {
		job := m.selectedJob(p)
		if job == nil {
			m.notice = "No CI check selected."
			return nil
		}
		var err error
		command, err = provider.JenkinsCommand(m.Config, *job)
		if err != nil {
			m.notice = core.Clean(err.Error())
			return nil
		}
		label = "Jenkins curl command"
	} else {
		command = provider.GitHubCommand(m.Config, p)
	}
	ctx, copy := m.Context, m.Actions.Copy
	return func() tea.Msg { return copyMsg{Err: copy(ctx, command), Label: label} }
}
