package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/mikeoertli/github-pr-monitor/internal/config"
	"github.com/mikeoertli/github-pr-monitor/internal/core"
)

type runnerFunc func(context.Context, string, ...string) ([]byte, error)

func (f runnerFunc) Run(ctx context.Context, path string, args ...string) ([]byte, error) {
	return f(ctx, path, args...)
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func response(code int, body string) *http.Response {
	return &http.Response{StatusCode: code, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
}
func graph(head, state string, nodes []map[string]any, more bool, cursor string) []byte {
	checks := map[string]any{"nodes": nodes, "pageInfo": map[string]any{"hasNextPage": more, "endCursor": cursor}}
	pr := map[string]any{"title": "Improve API", "state": state, "headRefOid": head, "commits": map[string]any{"nodes": []any{map[string]any{"commit": map[string]any{"statusCheckRollup": map[string]any{"contexts": checks}}}}}}
	b, _ := json.Marshal(map[string]any{"data": map[string]any{"repository": map[string]any{"pullRequest": pr}}})
	return b
}
func check(id, name, status string) map[string]any {
	return map[string]any{"__typename": "CheckRun", "id": id, "name": name, "status": status, "conclusion": "SUCCESS", "startedAt": "2026-09-16T12:00:00Z", "checkSuite": map[string]any{"app": map[string]any{"name": "Example CI", "slug": "example"}}}
}

func TestPRMetadataAndURLWarnings(t *testing.T) {
	g := New(config.Defaults())
	ref, _ := core.ParseRef("acme/api#1", "github.com")
	nodes := []map[string]any{check("1", "missing link", "QUEUED"), check("2", "invalid link", "COMPLETED")}
	nodes[1]["detailsUrl"] = "file:///tmp/build"
	var raw map[string]any
	json.Unmarshal(graph("abc", "OPEN", nodes, false, ""), &raw)
	pr := raw["data"].(map[string]any)["repository"].(map[string]any)["pullRequest"].(map[string]any)
	for k, v := range map[string]any{"headRefName": "feature/new", "baseRefName": "main", "author": map[string]any{"login": "alice"}, "isDraft": true, "reviewDecision": "CHANGES_REQUESTED", "mergeable": "CONFLICTING", "additions": 42, "deletions": 7, "changedFiles": 3, "comments": map[string]any{"totalCount": 5}} {
		pr[k] = v
	}
	pr["commits"].(map[string]any)["totalCount"] = 4
	b, _ := json.Marshal(raw)
	g.Runner = runnerFunc(func(_ context.Context, _ string, args ...string) ([]byte, error) {
		for _, field := range []string{"headRefName", "baseRefName", "reviewDecision", "changedFiles", "totalCount"} {
			if !strings.Contains(strings.Join(args, " "), field) {
				t.Fatalf("query missing %s", field)
			}
		}
		return b, nil
	})
	p := core.NewPR(ref, time.Now())
	p.Apply(g.Fetch(context.Background(), ref), time.Now())
	d := p.Details
	if d.Branch != "feature/new" || d.BaseBranch != "main" || d.Author != "alice" || !d.Draft || d.Additions != 42 || d.Commits != 4 || d.Comments != 5 || d.ReviewDecision != "CHANGES_REQUESTED" {
		t.Fatalf("metadata lost: %+v", d)
	}
	if len(p.Warnings()) != 2 || !strings.Contains(p.Jobs[0].Warning, "did not report") || !strings.Contains(p.Jobs[1].Warning, "invalid") {
		t.Fatalf("warnings lost: %+v", p.Jobs)
	}
	if got := core.Sorted([]core.PR{p}, "feature/new", "repo", false); len(got) != 1 {
		t.Fatal("branch filtering failed")
	}
	p.Apply(core.PR{Error: "offline"}, time.Now())
	if p.Details != d {
		t.Fatal("failed refresh discarded metadata")
	}
	if warnings := (core.PR{}).Warnings(); len(warnings) != 0 {
		t.Fatal("PR without CI generated a CI warning")
	}
}
func TestGitHubPaginationAndHeadChange(t *testing.T) {
	g := New(config.Defaults())
	ref, _ := core.ParseRef("acme/api#1", "github.com")
	calls := 0
	g.Runner = runnerFunc(func(ctx context.Context, path string, args ...string) ([]byte, error) {
		calls++
		joined := strings.Join(args, " ")
		if !strings.Contains(joined, "--hostname github.com") || !strings.Contains(joined, "number=1") {
			t.Fatal(joined)
		}
		if _, ok := ctx.Deadline(); !ok {
			t.Fatal("missing request timeout")
		}
		if calls == 1 {
			return graph("a", "OPEN", []map[string]any{check("1", "test", "COMPLETED")}, true, "next"), nil
		}
		if !strings.Contains(joined, "cursor=next") {
			t.Fatal("missing pagination cursor")
		}
		return graph("a", "OPEN", []map[string]any{check("2", "lint", "QUEUED")}, false, ""), nil
	})
	p := g.Fetch(context.Background(), ref)
	if p.Error != "" || len(p.Jobs) != 2 || p.Jobs[0].Status != "passed" || p.Jobs[1].Status != "queued" {
		t.Fatalf("%+v", p)
	}
	calls = 0
	g.Runner = runnerFunc(func(context.Context, string, ...string) ([]byte, error) {
		calls++
		head := "a"
		if calls == 2 {
			head = "b"
		}
		return graph(head, "OPEN", nil, calls == 1, "next"), nil
	})
	p = g.Fetch(context.Background(), ref)
	if !strings.Contains(p.Error, "changed") {
		t.Fatalf("mixed heads accepted: %+v", p)
	}
}
func TestGitHubFailuresAndNoChecks(t *testing.T) {
	g := New(config.Defaults())
	ref, _ := core.ParseRef("acme/api#1", "github.com")
	for _, raw := range []string{"bad json", `{"errors":[{"message":"rate limited"}]}`, `{"data":{"repository":{"pullRequest":null}}}`} {
		g.Runner = runnerFunc(func(context.Context, string, ...string) ([]byte, error) { return []byte(raw), nil })
		if p := g.Fetch(context.Background(), ref); p.Error == "" {
			t.Fatal("accepted invalid response")
		}
	}
	g.Runner = runnerFunc(func(context.Context, string, ...string) ([]byte, error) {
		return graph("a", "OPEN", nil, false, ""), nil
	})
	p := g.Fetch(context.Background(), ref)
	if p.Error != "" || len(p.Jobs) != 0 {
		t.Fatal(p)
	}
}
func TestActionsStepsAndRunIdentity(t *testing.T) {
	g := New(config.Defaults())
	ref, _ := core.ParseRef("acme/api#1", "github.com")
	n := check("stable-node-id", "test", "IN_PROGRESS")
	n["detailsUrl"] = "https://github.com/acme/api/actions/runs/123/job/456"
	n["checkSuite"] = map[string]any{"app": map[string]any{"name": "GitHub Actions", "slug": "github-actions"}, "workflowRun": map[string]any{"workflow": map[string]any{"name": "CI"}}}
	failDetails := false
	g.Runner = runnerFunc(func(ctx context.Context, path string, args ...string) ([]byte, error) {
		s := strings.Join(args, " ")
		if strings.Contains(s, "graphql") {
			return graph("a", "OPEN", []map[string]any{n}, false, ""), nil
		}
		if failDetails {
			return nil, errors.New("unavailable")
		}
		if strings.Contains(s, "/jobs/456") {
			return []byte(`{"run_attempt":2,"status":"in_progress","steps":[{"name":"Setup","status":"completed"},{"name":"Unit tests","status":"in_progress"}]}`), nil
		}
		if strings.Contains(s, "/runs/123") {
			return []byte(`{"run_number":42}`), nil
		}
		t.Fatal(s)
		return nil, nil
	})
	p := g.Fetch(context.Background(), ref)
	j := p.Jobs[0]
	if len(p.GitHubRequests) != 3 || !strings.Contains(strings.Join(p.GitHubRequests[1], " "), "/jobs/456") || !strings.Contains(strings.Join(p.GitHubRequests[2], " "), "/runs/123") {
		t.Fatal("Actions source requests not recorded")
	}
	if j.Number != "42.2" || j.Progress != .5 || j.Phase != "Unit tests" || j.Name != "CI / test" || j.RunID != "stable-node-id" {
		t.Fatalf("%+v", j)
	}
	failDetails = true
	p = g.Fetch(context.Background(), ref)
	if p.Jobs[0].RunID != j.RunID || p.Jobs[0].Warning == "" {
		t.Fatal("detail failure changed identity")
	}
}
func TestStatusContextIdentity(t *testing.T) {
	n := checkNode{Type: "StatusContext", Context: "build", State: "PENDING", ID: "context-1", TargetURL: "https://ci.example.com/build/2"}
	a := jobFromNode(n, "head")
	n.ID = "context-2"
	n.State = "SUCCESS"
	b := jobFromNode(n, "head")
	if a.RunID != b.RunID || b.Status != "passed" || b.Progress != 1 {
		t.Fatalf("%+v %+v", a, b)
	}
}
func TestJenkinsEstimateStageAndCredentialScope(t *testing.T) {
	c := config.Defaults()
	c.Jenkins = []config.Jenkins{{URL: "https://ci.example.com/jenkins", User: "inline", Token: "inline", UserEnv: "GPRM_TEST_USER", TokenEnv: "GPRM_TEST_TOKEN"}}
	t.Setenv("GPRM_TEST_USER", "tester")
	t.Setenv("GPRM_TEST_TOKEN", "secret")
	j := NewJenkins(c)
	calls := 0
	j.Client.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		user, token, ok := r.BasicAuth()
		if strings.HasPrefix(r.URL.Path, "/jenkins/") {
			if !ok || user != "tester" || token != "secret" {
				t.Fatal("credential mismatch")
			}
		} else if ok {
			t.Fatal("credentials escaped configured path")
		}
		switch {
		case strings.HasSuffix(r.URL.Path, "/lastSuccessfulBuild/api/json"):
			return response(200, `{"duration":600000}`), nil
		case strings.HasSuffix(r.URL.Path, "/wfapi/describe"):
			return response(200, `{"stages":[{"name":"Tests","status":"IN_PROGRESS"},{"name":"Lint","status":"IN_PROGRESS"}]}`), nil
		default:
			return response(200, fmt.Sprintf(`{"number":42,"building":true,"timestamp":%d,"estimatedDuration":100000}`, time.Now().Add(-3*time.Minute).UnixMilli())), nil
		}
	})
	job := core.Job{URL: "https://ci.example.com/jenkins/job/api/42/", Provider: "Jenkins", Name: "Build", Status: "queued"}
	got := j.Enrich(context.Background(), job)
	if got.Stale || got.Warning != "" || !got.Estimated || got.Progress < .29 || got.Progress > .31 || got.Phase != "Tests + Lint" || got.Number != "42" {
		t.Fatalf("%+v", got)
	}
	if len(got.JenkinsRequests) != 3 {
		t.Fatal("missing Jenkins source requests")
	}
	before := calls
	cached := j.Enrich(context.Background(), job)
	if !reflect.DeepEqual(cached.JenkinsRequests, got.JenkinsRequests) {
		t.Fatal("cached estimate source missing from copied requests")
	}
	if calls-before != 2 {
		t.Fatal("estimate not cached per build")
	}
	var out any
	if err := j.get(context.Background(), "https://ci.example.com/unrelated/api/json", &out); err != nil {
		t.Fatal(err)
	}
}
func TestJenkinsSupersededAndOptionalStages(t *testing.T) {
	j := NewJenkins(config.Defaults())
	j.Client.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if strings.HasSuffix(r.URL.Path, "/1/api/json") {
			return response(200, `{"number":1,"building":false,"result":"ABORTED","nextBuild":{"url":"https://ci.example.com/job/api/2/"}}`), nil
		}
		if strings.HasSuffix(r.URL.Path, "/wfapi/describe") {
			return response(404, ``), nil
		}
		return response(200, `{"number":2,"building":false,"result":"SUCCESS","duration":12000,"timestamp":1000}`), nil
	})
	got := j.Enrich(context.Background(), core.Job{URL: "https://ci.example.com/job/api/1/", Provider: "Jenkins"})
	if len(got.JenkinsRequests) != 3 || !strings.Contains(got.JenkinsRequests[1], "/2/api/json") {
		t.Fatal("superseding request not recorded")
	}
	if got.Number != "2" || got.Status != "passed" || got.Warning != "" || got.Progress != 1 || got.Duration != 12*time.Second {
		t.Fatal(got)
	}
	p := core.NewPR(core.Ref{}, time.Now())
	p.Apply(core.PR{Jobs: []core.Job{got}}, time.Now())
	if len(p.History) != 2 {
		t.Fatalf("superseded build missing from history: %+v", p.History)
	}
	p.Apply(core.PR{Jobs: []core.Job{got}}, time.Now())
	if len(p.History) != 2 {
		t.Fatal("superseded history counted twice")
	}
}
func TestJenkinsFailuresAndRedirects(t *testing.T) {
	j := NewJenkins(config.Defaults())
	job := core.Job{URL: "https://ci.example.com/job/api/1/", Status: "passed"}
	for _, body := range []string{`{}`, `{"building":false,"number":1}`, `not json`} {
		j.Client.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) { return response(200, body), nil })
		got := j.Enrich(context.Background(), job)
		if !got.Stale || got.Warning == "" {
			t.Fatal(got)
		}
	}
	calls := 0
	j.Client.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		resp := response(302, "")
		resp.Header.Set("Location", "https://elsewhere.example.com/api/json")
		return resp, nil
	})
	got := j.Enrich(context.Background(), job)
	if calls != 1 || !got.Stale {
		t.Fatalf("followed redirect: %d %+v", calls, got)
	}
}
func TestDiscovery(t *testing.T) {
	g := New(config.Defaults())
	g.Config.GitHubHost = "git.example.com"
	g.Runner = runnerFunc(func(ctx context.Context, path string, args ...string) ([]byte, error) {
		s := strings.Join(args, " ")
		if !strings.Contains(s, "--hostname git.example.com") {
			t.Fatal(s)
		}
		if args[len(args)-1] == "user" {
			return []byte(`{"login":"someone"}`), nil
		}
		if !strings.Contains(s, "author:someone") || (!strings.Contains(s, "is:open") && !strings.Contains(s, "is:closed")) {
			t.Fatal(s)
		}
		return []byte(`{"items":[{"pull_request":{"html_url":"https://git.example.com/acme/api/pull/2"}}]}`), nil
	})
	refs, err := g.Discover(context.Background())
	if err != nil || len(refs) != 1 || refs[0].Host != "git.example.com" {
		t.Fatalf("%+v %v", refs, err)
	}
}

func TestDiscoveryIncludesRecentClosures(t *testing.T) {
	for _, retention := range []string{"24h", "forever", "0s"} {
		g := New(config.Defaults())
		g.Config.CompletedRetention = retention
		closedSearches := 0
		g.Runner = runnerFunc(func(_ context.Context, _ string, args ...string) ([]byte, error) {
			if args[len(args)-1] == "user" {
				return []byte(`{"login":"someone"}`), nil
			}
			if strings.Contains(strings.Join(args, " "), "is:closed") {
				closedSearches++
				if !strings.Contains(strings.Join(args, " "), "closed:>=") {
					t.Fatal("unbounded closed search")
				}
				return []byte(fmt.Sprintf(`{"items":[{"closed_at":%q,"pull_request":{"html_url":"https://github.com/acme/api/pull/2"}},{"closed_at":%q,"pull_request":{"html_url":"https://github.com/acme/api/pull/3"}},{"pull_request":{"html_url":"https://github.com/acme/api/pull/1"}}]}`, time.Now().Add(-time.Hour).Format(time.RFC3339), time.Now().Add(-25*time.Hour).Format(time.RFC3339))), nil
			}
			return []byte(`{"items":[{"pull_request":{"html_url":"https://github.com/acme/api/pull/1"}}]}`), nil
		})
		refs, err := g.Discover(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if retention == "0s" {
			if len(refs) != 1 || closedSearches != 0 {
				t.Fatal("zero retention discovered closures")
			}
		} else if len(refs) != 2 || refs[1].Number != 2 || closedSearches != 1 {
			t.Fatal("recent closure missing or old/duplicate included")
		}
	}
}

func TestQueuedRerunDoesNotDisappearBehindCompletedRun(t *testing.T) {
	g := New(config.Defaults())
	ref, _ := core.ParseRef("acme/api#1", "github.com")
	complete := check("old", "tests", "COMPLETED")
	pending := check("new", "tests", "QUEUED")
	pending["startedAt"] = nil
	for _, nodes := range [][]map[string]any{{complete, pending}, {pending, complete}} {
		g.Runner = runnerFunc(func(context.Context, string, ...string) ([]byte, error) {
			return graph("a", "OPEN", nodes, false, ""), nil
		})
		p := g.Fetch(context.Background(), ref)
		if p.Error != "" || len(p.Jobs) != 1 || p.Jobs[0].Status != "queued" {
			t.Fatalf("queued rerun hidden: %+v", p)
		}
		tracked := core.NewPR(ref, time.Now())
		tracked.Apply(p, time.Now())
		if core.ShouldQuit([]core.PR{tracked}, "all-passing") {
			t.Fatal("queued rerun triggered exit")
		}
	}
}

func TestJenkinsOverdueKeepsReferenceDuration(t *testing.T) {
	j := NewJenkins(config.Defaults())
	j.Client.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if strings.Contains(r.URL.Path, "lastSuccessfulBuild") {
			return response(404, ""), nil
		}
		if strings.HasSuffix(r.URL.Path, "wfapi/describe") {
			return response(200, `{"stages":[{"name":"Tests","status":"IN_PROGRESS"}]}`), nil
		}
		return response(200, fmt.Sprintf(`{"number":7,"building":true,"timestamp":%d,"estimatedDuration":600000}`, time.Now().Add(-15*time.Minute).UnixMilli())), nil
	})
	got := j.Enrich(context.Background(), core.Job{URL: "https://ci.example.com/job/api/7/", Provider: "Jenkins"})
	if got.Progress != .99 || !got.Estimated || got.ExpectedDuration != 10*time.Minute || got.Status != "running" || !strings.Contains(got.Phase, "Tests · +") || !strings.Contains(got.Phase, "over estimate") {
		t.Fatalf("bad overrun state: %+v", got)
	}
}
