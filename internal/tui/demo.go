package tui

import (
	"fmt"
	"time"

	"github.com/mikeoertli/github-pr-monitor/internal/core"
)

func DemoPRs() []core.PR {
	var prs []core.PR
	for _, raw := range []string{"acme/platform#142", "acme/web#87", "acme/worker#36", "acme/docs#19"} {
		ref, _ := core.ParseRef(raw, "github.com")
		prs = append(prs, core.NewPR(ref, time.Now()))
	}
	return prs
}
func DemoSnapshots(refs []core.Ref, step int) []core.PR {
	var prs []core.PR
	for i, ref := range refs {
		progress := min(.96, .20+float64((step+i*3)%20)*.035)
		job := core.Job{Key: "Jenkins/build", RunID: "build-42", Name: "Build and test", Provider: "Jenkins", Number: "42", URL: "https://ci.example.com/job/platform/42/", Status: "running", Phase: "Integration tests", Progress: progress, Estimated: true, StartedAt: time.Now().Add(-time.Duration(progress*600) * time.Second)}
		title := "Improve deployment validation"
		state := "OPEN"
		switch i % 4 {
		case 1:
			job = core.Job{Key: "GitHub Actions/test", RunID: "run-218", Name: "Test / linux", Provider: "GitHub Actions", Number: "218", URL: ref.URL + "/checks", Status: "running", Phase: "Run unit tests", Progress: .6, StartedAt: time.Now().Add(-2 * time.Minute)}
			title = "Add keyboard navigation"
		case 2:
			job.Status = "failed"
			job.Phase = "Integration tests failed"
			job.Progress = 1
			job.Duration = 9 * time.Minute
			job.Estimated = false
			title = "Handle retry backoff"
		case 3:
			job = core.Job{Key: "CircleCI/docs", RunID: "run-31", Name: "Docs", Provider: "CircleCI", Number: "31", URL: "https://app.circleci.com/", Status: "passed", Phase: "passed", Progress: 1, Duration: 50 * time.Second}
			title = "Refresh getting started guide"
			state = "MERGED"
		}
		prs = append(prs, core.PR{Ref: ref, Title: title, Head: fmt.Sprintf("demo-%d", i), State: state, Jobs: []core.Job{job}})
	}
	return prs
}
