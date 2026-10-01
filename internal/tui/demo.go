package tui

import (
	"fmt"
	"time"

	"github.com/mikeoertli/github-pr-monitor/internal/core"
)

// Anchor fixture times once so refreshing the demo does not reset event ages.
var demoEpoch = time.Now().Truncate(time.Second)

func DemoPRs() []core.PR {
	var prs []core.PR
	for _, raw := range []string{"acme/platform#142", "acme/web#87", "acme/worker#36", "acme/docs#19", "acme/service#63"} {
		ref, _ := core.ParseRef(raw, "github.com")
		prs = append(prs, core.NewPR(ref, time.Now()))
	}
	return prs
}
func DemoSnapshots(refs []core.Ref, step int) []core.PR {
	var prs []core.PR
	fixtures := DemoPRs()
	for i, ref := range refs {
		// Keep each example stable when filtering or sorting changes request order.
		for slot, fixture := range fixtures {
			if fixture.Ref.URL == ref.URL {
				i = slot
				break
			}
		}
		progress := min(.96, .20+float64((step+i*3)%20)*.035)
		job := core.Job{Key: "Jenkins/build", RunID: "build-42", Name: "Build and test", Provider: "Jenkins", Number: "42", URL: "https://ci.example.com/job/platform/42/", Status: "running", Phase: "Integration tests", Progress: progress, Estimated: true, StartedAt: demoEpoch.Add(-4 * time.Minute)}
		job.ExpectedDuration = 10 * time.Minute
		title := "ABC-42: Improve deployment validation"
		state := "OPEN"
		switch i % 5 {
		case 1:
			job = core.Job{Key: "GitHub Actions/test", RunID: "run-218", Name: "Test / linux", Provider: "GitHub Actions", Number: "218", URL: ref.URL + "/checks", Status: "running", Phase: "Run unit tests", Progress: .6, StartedAt: demoEpoch.Add(-2 * time.Minute)}
			title = "ABC-42: Add keyboard navigation"
		case 2:
			job.Status = "failed"
			job.Phase = "Integration tests failed"
			job.Progress = 1
			job.Duration = 9 * time.Minute
			job.CompletedAt = demoEpoch.Add(-90 * time.Minute)
			job.StartedAt = job.CompletedAt.Add(-job.Duration)
			job.Estimated = false
			title = "OPS-7: Handle retry backoff"
		case 3:
			job = core.Job{Key: "CircleCI/docs", RunID: "run-31", Name: "Docs", Provider: "CircleCI", Number: "31", URL: "https://app.circleci.com/", Status: "passed", Phase: "passed", Progress: 1, Duration: 50 * time.Second}
			title = "Refresh getting started guide"
			state = "MERGED"
		case 4:
			job.StartedAt = demoEpoch.Add(-11 * time.Minute)
			job.Progress = .99
			job.Phase = "Integration tests · +1m0s over estimate"
			title = "Validate service startup"
		}
		if i%5 == 3 {
			job.CompletedAt = demoEpoch.Add(-40 * time.Minute)
			job.StartedAt = job.CompletedAt.Add(-job.Duration)
		}
		details := core.PRDetails{Branch: fmt.Sprintf("feature/improvement-%d", ref.Number), BaseBranch: "main", Author: "demo-user", ReviewDecision: "APPROVED", Mergeable: "MERGEABLE", Additions: 142, Deletions: 38, ChangedFiles: 7, Commits: 3, Comments: 4, CreatedAt: demoEpoch.Add(-24 * time.Hour), UpdatedAt: demoEpoch.Add(-5 * time.Minute)}
		if i%5 == 3 {
			details.MergedAt = demoEpoch.Add(-35 * time.Minute)
		}
		if i%5 == 0 {
			details.ViewerCanUpdateBranch = true
			details.BaseBranch = "release/1"
		}
		if i%5 == 4 {
			job.Warning = "Jenkins pipeline stages unavailable (HTTP 403); using build status"
		}
		prs = append(prs, core.PR{Ref: ref, Title: title, Head: fmt.Sprintf("demo-%d", i), State: state, Details: details, Jobs: []core.Job{job}})
	}
	return prs
}
