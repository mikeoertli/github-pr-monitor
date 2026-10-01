package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/mikeoertli/github-pr-monitor/internal/core"
)

func TestStatusAgeInTableDetails(t *testing.T) {
	m := demoModel()
	m.Config.NoColor = true
	m.width = 200
	m.height = 40
	m.SetFilter("worker")
	p := m.PRs[2]
	if !strings.Contains(m.View(), "failed 90m ago") {
		t.Fatal("status age missing")
	}
	detail := strings.Join(m.details(p, true), "\n")
	if !strings.Contains(detail, "provider event") {
		t.Fatal(detail)
	}

	p.Apply(core.PR{Error: "offline"}, time.Now())
	detail = strings.Join(m.details(p, true), "\n")
	if !strings.Contains(detail, "Status: failed 90m ago") || strings.Contains(detail, "Status: stale 90m ago") {
		t.Fatal(detail)
	}
}
