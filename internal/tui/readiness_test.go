package tui

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/mikeoertli/github-pr-monitor/internal/core"
)

func TestNoChecksTableDetailsAndJSON(t *testing.T) {
	m := demoModel()
	m.Config.NoColor = true
	m.width, m.height = 170, 40
	m.PRs = m.PRs[:1]
	p := &m.PRs[0]
	p.Jobs = nil
	p.Details.Mergeable, p.Details.MergeStateStatus = "CONFLICTING", "DIRTY"
	m.expanded[p.Ref.URL] = true
	view := m.View()
	for _, want := range []string{"conflicts", "Not mergeable: merge conflicts", "Merge state: DIRTY"} {
		if !strings.Contains(view, want) {
			t.Fatalf("missing %q in no-check view", want)
		}
	}
	for _, unwanted := range []string{"Waiting for checks", "No CI checks reported yet", "no checks", "⚠"} {
		if strings.Contains(view, unwanted) {
			t.Fatalf("misleading no-check display: %q", unwanted)
		}
	}
	if m.bar(*p, false, time.Now()) != "—" {
		t.Fatal("no-check PR should not have an empty progress bar")
	}
	b, err := json.Marshal(exportSnapshot(*p))
	if err != nil || !strings.Contains(string(b), `"status":"conflicts"`) || !strings.Contains(string(b), `"MergeStateStatus":"DIRTY"`) {
		t.Fatal("JSON does not match merge readiness")
	}
	p.Details.Mergeable, p.Details.MergeStateStatus = "MERGEABLE", "CLEAN"
	if !strings.Contains(m.View(), "No CI checks · Mergeable") {
		t.Fatal("ready PR not shown as mergeable")
	}
	p.Fresh = false
	p.Details = core.PRDetails{}
	if !strings.Contains(m.View(), "Loading PR status") {
		t.Fatal("unfetched PR incorrectly treated as ready")
	}
}
