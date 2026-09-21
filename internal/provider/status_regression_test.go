package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/mikeoertli/github-pr-monitor/internal/config"
	"github.com/mikeoertli/github-pr-monitor/internal/core"
)

func TestJenkinsBuildAndReportURLs(t *testing.T) {
	root := "https://ci.example.com/jenkins/job/platform/job/feature%2Fwork/43"
	for _, suffix := range []string{"", "/", "/display/redirect", "/display/redirect/", "/display/redirect?page=tests", "/display/redirect/?page=coverage#summary", "//coverage", "/coverage/", "/testReport/package/class/17/", "/artifact/results/12/index.html", "/?view=summary#result", "/display/redirect?url=https%3A%2F%2Felsewhere.example.com"} {
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
			if err != nil || !strings.Contains(command, root+"/api/json") || strings.Contains(command, "display/redirect") || strings.Contains(command, "/coverage") || strings.Contains(command, "page=") || strings.Contains(command, "elsewhere.example.com") {
				t.Fatalf("bad copied command: %v", err)
			}
		}
		j.Client.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) { return response(403, `{}`), nil })
		failed := j.Enrich(context.Background(), job)
		if failed.Number != "43" || !failed.Stale || !strings.Contains(failed.Warning, "403") {
			t.Fatal("failed lookup lost URL-derived build number")
		}
	}
	for _, raw := range []string{
		"https://ci.example.com/job/api/",
		"https://ci.example.com/job/api/lastBuild/coverage",
		"https://ci.example.com/job/api/coverage/17/",
		"https://ci.example.com/job/api/0/coverage",
		"https://ci.example.com/job/api/43x/coverage",
		"https://ci.example.com/job//43/coverage",
		"https://ci.example.com/job/api/43/../coverage",
		"https://ci.example.com/job/%2e%2e/43/coverage",
		"https://ci.example.com/job/api/43/%2e%2e/coverage",
		"https://ci.example.com/job/api/43/%invalid",
		"https://user:secret@ci.example.com/job/api/43/coverage",
		"file:///job/api/43/coverage",
		"https://ci.example.com/blue/organizations/jenkins/api/43",
	} {
		if _, err := jenkinsBuildRoot(raw); err == nil {
			t.Fatal("accepted invalid or non-build URL")
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

func TestGitHubGroupsJenkinsReportsWithRunningBuild(t *testing.T) {
	for _, overview := range []string{"/", "/display/redirect"} {
		for _, offline := range []bool{false, true} {
			for _, reportFirst := range []bool{false, true} {
				t.Run(fmt.Sprintf("overview=%s/offline=%t/reportFirst=%t", overview, offline, reportFirst), func(t *testing.T) {
					g := New(config.Defaults())
					ref, _ := core.ParseRef("acme/api#1", "github.com")
					root := "https://ci.example.com/jenkins/job/team/job/api/job/PR-1/43"
					build := map[string]any{"__typename": "StatusContext", "id": "build", "context": "Build", "state": "PENDING", "targetUrl": root + overview}
					coverage := check("coverage", "Coverage", "COMPLETED")
					coverage["detailsUrl"] = root + "//coverage"
					tests := check("tests", "Tests", "COMPLETED")
					tests["detailsUrl"] = root + "/display/redirect?page=tests"
					nodes := []map[string]any{build, coverage, tests}
					if reportFirst {
						nodes = []map[string]any{coverage, tests, build}
					}
					g.Runner = runnerFunc(func(context.Context, string, ...string) ([]byte, error) {
						return graph("head", "OPEN", nodes, false, ""), nil
					})
					var requests []string
					g.Jenkins.Client.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
						requests = append(requests, req.URL.String())
						if offline {
							return response(503, `{}`), nil
						}
						switch req.URL.String() {
						case root + "/api/json":
							return response(200, fmt.Sprintf(`{"number":43,"building":true,"timestamp":%d,"estimatedDuration":600000}`, time.Now().Add(-3*time.Minute).UnixMilli())), nil
						case estimateURL(root):
							return response(200, `{"duration":600000}`), nil
						case root + "/wfapi/describe":
							return response(200, `{"stages":[{"name":"Integration tests","status":"IN_PROGRESS"}]}`), nil
						default:
							t.Fatalf("unexpected API request %s", req.URL.String())
							return nil, nil
						}
					})
					p := g.Fetch(context.Background(), ref)
					if p.Error != "" || len(p.Jobs) != 1 {
						t.Fatalf("report links produced duplicate builds: %+v", p)
					}
					job := p.Jobs[0]
					wantStatus := "running"
					if offline {
						wantStatus = "queued"
					}
					if job.Name != "Build" || job.Number != "43" || job.Status != wantStatus || job.RunID != root || job.URL != root+"/" {
						t.Fatalf("lost parent build identity/status: %+v", job)
					}
					if offline {
						if len(requests) != 1 || !job.Stale || !strings.Contains(job.Warning, "503") {
							t.Fatal("unavailable Jenkins build must retain its pending GitHub fallback and warning")
						}
					} else if len(requests) != 3 || job.Warning != "" || job.Phase != "Integration tests" || p.Progress() < .29 || p.Progress() > .31 {
						t.Fatalf("duplicate reports inflated progress or triggered warnings: %+v, requests %v", job, requests)
					}
				})
			}
		}
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

func TestGitHubNoChecksUsesMergeState(t *testing.T) {
	g := New(config.Defaults())
	ref, _ := core.ParseRef("acme/docs#42", "github.com")
	g.Runner = runnerFunc(func(_ context.Context, _ string, args ...string) ([]byte, error) {
		if !strings.Contains(strings.Join(args, " "), "mergeStateStatus") {
			t.Fatal("query omitted merge state")
		}
		return []byte(`{"data":{"repository":{"pullRequest":{"title":"Update guide","state":"OPEN","headRefOid":"head","mergeable":"CONFLICTING","mergeStateStatus":"DIRTY","commits":{"totalCount":1,"nodes":[{"commit":{"statusCheckRollup":null}}]}}}}}`), nil
	})
	p := g.Fetch(context.Background(), ref)
	if p.Error != "" || len(p.Jobs) != 0 || p.Status() != "conflicts" || p.Details.MergeStateStatus != "DIRTY" {
		t.Fatalf("no-check merge state was lost: %+v", p)
	}
}
