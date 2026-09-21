package tui

import (
	"context"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mikeoertli/github-pr-monitor/internal/core"
)

type filterSource struct {
	mu    sync.Mutex
	calls []core.Ref
	refs  []core.Ref
}

func (s *filterSource) Fetch(_ context.Context, ref core.Ref) core.PR {
	s.mu.Lock()
	s.calls = append(s.calls, ref)
	s.mu.Unlock()
	return core.PR{Ref: ref, State: "OPEN", Head: "head"}
}
func (s *filterSource) Discover(context.Context) ([]core.Ref, error) { return s.refs, nil }

func TestFilterScopesPollingAndDiscovery(t *testing.T) {
	m := demoModel()
	source := &filterSource{}
	m.Demo, m.Source = false, source
	m.SetFilter("PLAT #142")
	if len(m.PollingRows()) != 1 {
		t.Fatal("multi-term fuzzy filter failed")
	}
	m.Update(m.poll()())
	if len(source.calls) != 1 || source.calls[0].Repo != "acme/platform" {
		t.Fatal(source.calls)
	}
	m.SetFilter("no-match")
	if m.poll() != nil {
		t.Fatal("empty filter match started a request")
	}
	for _, raw := range []string{"acme/platform#999", "acme/unrelated#888"} {
		ref, _ := core.ParseRef(raw, "github.com")
		source.refs = append(source.refs, ref)
	}
	m.SetFilter("plat")
	_, cmd := m.Update(m.discover()())
	if len(m.PRs) != 7 {
		t.Fatal("filter removed a discovery result")
	}
	m.Update(cmd())
	for _, ref := range source.calls {
		if ref.Repo != "acme/platform" {
			t.Fatal("polled excluded discovered PR")
		}
	}
	if len(source.calls) != 3 {
		t.Fatal("matching discovery result not polled")
	}
}

func TestFilterEditingAndClearingRefreshPollingScope(t *testing.T) {
	m := demoModel()
	m.SetFilter("platform")
	m.Update(key("/"))
	m.input.SetValue("web")
	_, cmd := m.Update(key("enter"))
	if cmd == nil {
		t.Fatal("applying filter did not refresh")
	}
	msg := cmd().(pollMsg)
	if len(msg.PRs) != 1 || msg.PRs[0].Ref.Repo != "acme/web" {
		t.Fatal("edit did not change polling scope")
	}
	m.Update(msg)
	m.Update(key("/"))
	m.input.SetValue("docs")
	m.SetFilter("docs") // Live input preview.
	_, cmd = m.Update(key("esc"))
	if m.filter != "web" {
		t.Fatal("cancel did not restore previous filter")
	}
	m.Update(cmd())
	_, cmd = m.Update(key("esc"))
	if m.filter != "" || cmd == nil {
		t.Fatal("clearing filter did not refresh all PRs")
	}
	msg = cmd().(pollMsg)
	if len(msg.PRs) != len(m.PRs) {
		t.Fatal("clearing left PRs unpolled")
	}
	m.Update(msg)
	m.paused = true
	m.Update(key("/"))
	m.input.SetValue("platform")
	_, cmd = m.Update(key("enter"))
	if cmd != nil {
		t.Fatal("filter ignored pause")
	}
}

func TestFilterChangeDuringPollQueuesNewScope(t *testing.T) {
	m := demoModel()
	m.SetFilter("platform")
	inflight := m.poll()
	m.Update(key("/"))
	m.input.SetValue("web")
	_, cmd := m.Update(key("enter"))
	if cmd != nil || !m.refreshAgain {
		t.Fatal("filter did not queue behind in-flight poll")
	}
	_, cmd = m.Update(inflight())
	if cmd == nil {
		t.Fatal("new scope never refreshed")
	}
	msg := cmd().(pollMsg)
	if len(msg.PRs) != 1 || msg.PRs[0].Ref.Repo != "acme/web" {
		t.Fatal("queued poll kept old filter")
	}
}

func TestFilterRequiresFreshDataWhenReincluded(t *testing.T) {
	m := demoModel()
	m.Config.AutoQuit = "all-closed"
	for i := range m.PRs {
		m.PRs[i].State = "MERGED"
	}
	m.SetFilter("docs")
	m.SetFilter("")
	if core.ShouldQuit(m.scopedPRs(), m.Config.AutoQuit) {
		t.Fatal("unpolled restored scope triggered quit")
	}
	// Hidden PRs accumulate no monitoring time and stay out of the summary.
	m.SetFilter("docs")
	i := m.selected()
	before := make([]time.Duration, len(m.PRs))
	for n := range m.PRs {
		before[n] = m.PRs[n].Monitored
	}
	m.lastTick = time.Now().Add(-time.Minute)
	m.account(time.Now())
	for n := range m.PRs {
		if n != i && m.PRs[n].Monitored != before[n] {
			t.Fatal("hidden PR accumulated monitoring time")
		}
	}
	m.PRs[i].Removed = true
	if rows := m.SummaryPRs(); len(rows) != 1 || rows[0].Ref.Repo != "acme/docs" {
		t.Fatal("filtered summary lost dismissed PR or included hidden PRs")
	}
}

func TestFilterPreviewDoesNotPoll(t *testing.T) {
	m := demoModel()
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	m.lastPoll = time.Now().Add(-time.Hour)
	m.Update(tickMsg(time.Now()))
	if m.busy {
		t.Fatal("filter preview started polling before apply")
	}
}
