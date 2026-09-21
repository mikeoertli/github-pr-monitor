package tui

import (
	"encoding/json"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mikeoertli/github-pr-monitor/internal/core"
	"github.com/mikeoertli/github-pr-monitor/internal/provider"
)

// snapshot exports the latest displayed data, including explicit freshness after
// a failed update. Config, credentials, and historical runs are not part of it.
type snapshot struct {
	Ref         core.Ref       `json:"ref"`
	Title       string         `json:"title"`
	Head        string         `json:"head"`
	State       string         `json:"state"`
	Details     core.PRDetails `json:"details"`
	Jobs        []core.Job     `json:"jobs"`
	Status      string         `json:"status"`
	FinalStatus string         `json:"final_status"`
	Progress    float64        `json:"progress"`
	Fresh       bool           `json:"fresh"`
	LastAttempt time.Time      `json:"last_attempt"`
	LastSuccess time.Time      `json:"last_success"`
	Error       string         `json:"error,omitempty"`
	Warnings    []string       `json:"warnings,omitempty"`
}

func exportSnapshot(p core.PR) snapshot {
	return snapshot{Ref: p.Ref, Title: p.Title, Head: p.Head, State: p.State,
		Details: p.Details, Jobs: p.Jobs, Status: p.Status(), FinalStatus: p.FinalStatus(),
		Progress: p.Progress(), Fresh: p.Fresh, LastAttempt: p.LastAttempt,
		LastSuccess: p.LastSuccess, Error: p.Error, Warnings: p.Warnings()}
}

func (m *Model) copyJSON(all bool) tea.Cmd {
	rows := core.Sorted(m.PRs, m.filter, m.Config.Sort, m.Config.Descending)
	if !all {
		if i := m.selected(); i >= 0 {
			rows = []int{i}
		} else {
			rows = nil
		}
	}
	if len(rows) == 0 {
		m.notice = "No PRs to copy."
		return nil
	}
	if m.Actions.Copy == nil {
		m.notice = "Clipboard copying is unavailable."
		return nil
	}
	values := make([]snapshot, 0, len(rows))
	for _, i := range rows {
		values = append(values, exportSnapshot(m.PRs[i]))
	}
	var payload any = values
	if !all {
		payload = values[0]
	}
	b, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		m.notice = "Could not encode PR data as JSON."
		return nil
	}
	// Capture a complete snapshot before returning to the asynchronous update loop.
	ctx, copy := m.Context, m.Actions.Copy
	return func() tea.Msg { return copyMsg{Err: copy(ctx, string(b)+"\n"), Count: len(values)} }
}

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
