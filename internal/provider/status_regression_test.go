package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/mikeoertli/github-pr-monitor/internal/config"
	"github.com/mikeoertli/github-pr-monitor/internal/core"
)

func TestDisplayRedirectBuildURLs(t *testing.T) {
	root := "https://ci.example.com/jenkins/job/platform/job/feature%2Fwork/43"
	for _, suffix := range []string{"", "/", "/display/redirect", "/display/redirect/"} {
		got, err := jenkinsBuildRoot(root + suffix)
		if err != nil || got != root {
			t.Fatalf("normalization: %q %v", got, err)
		}
		j := NewJenkins(config.Defaults())
		job := core.Job{URL: root + suffix, Status: "passed", Progress: 1}
		if !j.Match(job) {
			t.Fatal("build link was not recognized without a provider name")
		}
		var requested []string
		j.Client.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
			requested = append(requested, req.URL.String())
			if strings.HasSuffix(req.URL.Path, "/wfapi/describe") {
				return response(200, `{"stages":[]}`), nil
			}
			return response(200, `{"number":43,"building":false,"result":"SUCCESS","timestamp":1726502400000,"duration":2513000}`), nil
		})
		gotJob := j.Enrich(context.Background(), job)
		if gotJob.Number != "43" || gotJob.Status != "passed" || gotJob.Warning != "" || gotJob.URL != root+"/" {
			t.Fatalf("bad build: %+v", gotJob)
		}
		if len(requested) != 2 || requested[0] != root+"/api/json" || requested[1] != root+"/wfapi/describe" {
			t.Fatal(requested)
		}
		for _, toCopy := range []core.Job{job, gotJob} {
			command, err := JenkinsCommand(config.Defaults(), toCopy)
			if err != nil || !strings.Contains(command, root+"/api/json") || strings.Contains(command, "display/redirect") {
				t.Fatalf("bad copied command: %v", err)
			}
		}
		j.Client.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) { return response(403, `{}`), nil })
		failed := j.Enrich(context.Background(), job)
		if failed.Number != "43" || !failed.Stale || !strings.Contains(failed.Warning, "403") {
			t.Fatal("failed lookup lost URL-derived build number")
		}
	}
	for _, suffix := range []string{"/display/redirect/extra", "/display/redirect?url=other", "/display/redirect#fragment", "/display/redirect/display/redirect"} {
		if _, err := jenkinsBuildRoot(root + suffix); err == nil {
			t.Fatalf("accepted ambiguous build URL %s", suffix)
		}
	}
}

func TestGitHubDeduplicatesDirectAndDisplayLinks(t *testing.T) {
	g := New(config.Defaults())
	ref, _ := core.ParseRef("acme/api#1", "github.com")
	first, second := check("1", "build", "COMPLETED"), check("2", "tests", "COMPLETED")
	first["detailsUrl"] = "https://ci.example.com/job/api/43/"
	second["detailsUrl"] = "https://ci.example.com/job/api/43/display/redirect"
	g.Runner = runnerFunc(func(context.Context, string, ...string) ([]byte, error) {
		return graph("head", "OPEN", []map[string]any{first, second}, false, ""), nil
	})
	calls := 0
	g.Jenkins.Client.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls++
		return response(200, `{"number":43,"building":false,"result":"SUCCESS"}`), nil
	})
	p := g.Fetch(context.Background(), ref)
	if p.Error != "" || len(p.Jobs) != 1 || p.Jobs[0].Number != "43" || calls != 2 {
		t.Fatalf("duplicate normalized build: %+v, calls %d", p, calls)
	}
}

func TestGitHubMergeFactsAndSourceTimestamps(t *testing.T) {
	updated := time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name, state        string
		merged, closed     bool
		mergedAt, closedAt string
		want               string
	}{
		{"merged enum", "MERGED", false, false, "", "", "MERGED"},
		{"merged flag", "OPEN", true, true, "", "", "MERGED"},
		{"merge timestamp", "OPEN", false, false, updated.Format(time.RFC3339), "", "MERGED"},
		{"closed flag", "OPEN", false, true, "", updated.Format(time.RFC3339), "CLOSED"},
		{"closed enum", "CLOSED", false, false, "", "", "CLOSED"},
		{"reopened", "OPEN", false, false, "", updated.Add(-time.Hour).Format(time.RFC3339), "OPEN"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := New(config.Defaults())
			ref, _ := core.ParseRef("acme/api#1", "github.com")
			var doc map[string]any
			json.Unmarshal(graph("head", tc.state, nil, false, ""), &doc)
			raw := doc["data"].(map[string]any)["repository"].(map[string]any)["pullRequest"].(map[string]any)
			raw["merged"], raw["closed"], raw["updatedAt"], raw["mergeable"] = tc.merged, tc.closed, updated.Format(time.RFC3339), "CONFLICTING"
			if tc.mergedAt != "" {
				raw["mergedAt"] = tc.mergedAt
			}
			if tc.closedAt != "" {
				raw["closedAt"] = tc.closedAt
			}
			b, _ := json.Marshal(doc)
			g.Runner = runnerFunc(func(_ context.Context, _ string, args ...string) ([]byte, error) {
				if !strings.Contains(strings.Join(args, " "), "merged mergedAt closed closedAt") {
					t.Fatal("missing explicit lifecycle fields")
				}
				return b, nil
			})
			before := time.Now()
			next := g.Fetch(context.Background(), ref)
			after := time.Now()
			p := core.NewPR(ref, before)
			p.Apply(next, after.Add(time.Minute))
			if p.Error != "" || p.State != tc.want || !p.Details.UpdatedAt.Equal(updated) {
				t.Fatalf("bad lifecycle: %+v", p)
			}
			if p.LastSuccess.Before(before) || p.LastSuccess.After(after) {
				t.Fatal("fetch timestamp replaced by update-loop time")
			}
			if core.ShouldQuit([]core.PR{p}, "all-closed") != (tc.want != "OPEN") {
				t.Fatal("closed/merged PR not handled by exit mode")
			}
		})
	}
}
