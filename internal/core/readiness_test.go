package core

import "testing"

func TestNoChecksReportsMergeReadiness(t *testing.T) {
	for _, tc := range []struct {
		name, state, mergeable, mergeState, review string
		draft                                      bool
		want                                       string
	}{
		{"ready", "OPEN", "MERGEABLE", "CLEAN", "", false, "mergeable"},
		{"older snapshot", "OPEN", "MERGEABLE", "", "", false, "mergeable"},
		{"conflicts", "OPEN", "CONFLICTING", "DIRTY", "", false, "conflicts"},
		{"dirty", "OPEN", "UNKNOWN", "DIRTY", "", false, "conflicts"},
		{"draft", "OPEN", "MERGEABLE", "CLEAN", "", true, "draft"},
		{"draft state", "OPEN", "MERGEABLE", "DRAFT", "", false, "draft"},
		{"blocked", "OPEN", "MERGEABLE", "BLOCKED", "", false, "blocked"},
		{"behind", "OPEN", "MERGEABLE", "BEHIND", "", false, "behind"},
		{"changes", "OPEN", "MERGEABLE", "CLEAN", "CHANGES_REQUESTED", false, "blocked"},
		{"review", "OPEN", "MERGEABLE", "CLEAN", "REVIEW_REQUIRED", false, "blocked"},
		{"unstable", "OPEN", "MERGEABLE", "UNSTABLE", "", false, "blocked"},
		{"unknown", "OPEN", "UNKNOWN", "UNKNOWN", "", false, "unknown"},
		{"computing", "OPEN", "MERGEABLE", "UNKNOWN", "", false, "unknown"},
		{"missing", "OPEN", "", "", "", false, "unknown"},
		{"merged", "MERGED", "CONFLICTING", "DIRTY", "", false, "merged"},
		{"closed", "CLOSED", "MERGEABLE", "CLEAN", "", false, "closed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := PR{State: tc.state, Fresh: true, Details: PRDetails{Mergeable: tc.mergeable, MergeStateStatus: tc.mergeState, ReviewDecision: tc.review, Draft: tc.draft}}
			if p.Status() != tc.want {
				t.Fatalf("got %s, want %s", p.Status(), tc.want)
			}
			if len(p.Warnings()) != 0 {
				t.Fatal("no-check PR incorrectly warned about a missing CI URL")
			}
			if p.Progress() != -1 {
				t.Fatal("mergeability should not invent CI progress")
			}
			if ShouldQuit([]PR{p}, "all-passing") || ShouldQuit([]PR{p}, "builds-finished") {
				t.Fatal("readiness was mistaken for a completed CI check")
			}
			p.Error = "offline"
			if p.Status() != "stale" {
				t.Fatal("fetch error was hidden by mergeability")
			}
			p.Error = ""
			p.Jobs = []Job{{Status: "running"}}
			if p.Status() != "building" {
				t.Fatal("mergeability replaced active CI status")
			}
		})
	}
}
