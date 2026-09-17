// Package provider reads GitHub and CI state. All remote operations are read-only.
package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/mikeoertli/github-pr-monitor/internal/config"
	"github.com/mikeoertli/github-pr-monitor/internal/core"
)

type Runner interface {
	Run(context.Context, string, ...string) ([]byte, error)
}
type ExecRunner struct{}

func (ExecRunner) Run(ctx context.Context, path string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Env = append(os.Environ(), "GH_PROMPT_DISABLED=1", "GH_PAGER=cat", "NO_COLOR=1")
	// Do not echo subprocess stderr: it can contain credentials or untrusted URLs.
	b, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("%s failed (check authentication, connectivity, and tool path): %w", core.Clean(path), err)
	}
	return b, nil
}

type GitHub struct {
	Config  config.Config
	Runner  Runner
	Jenkins *Jenkins
}

func New(c config.Config) *GitHub {
	return &GitHub{Config: c, Runner: ExecRunner{}, Jenkins: NewJenkins(c)}
}
func (g *GitHub) run(ctx context.Context, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, g.Config.RequestTimeout())
	defer cancel()
	return g.Runner.Run(ctx, g.Config.Tools.GH, args...)
}

const query = `query($owner:String!,$repo:String!,$number:Int!,$cursor:String) {
 repository(owner:$owner,name:$repo) { pullRequest(number:$number) {
  title state headRefOid merged mergedAt closed closedAt headRefName baseRefName author { login }
  isDraft reviewDecision mergeable additions deletions changedFiles createdAt updatedAt
  comments { totalCount }
  commits(last:1) { totalCount nodes { commit { statusCheckRollup { contexts(first:100,after:$cursor) {
   pageInfo { hasNextPage endCursor }
   nodes {
    __typename
    ... on CheckRun { id name status conclusion detailsUrl startedAt completedAt checkSuite { createdAt app { name slug } workflowRun { workflow { name } } } }
    ... on StatusContext { id context state targetUrl description createdAt }
   }
  } } } } }
 } }
}`

type checkNode struct {
	Type                                                                             string `json:"__typename"`
	ID, Name, Status, Conclusion, DetailsURL, Context, State, TargetURL, Description string
	StartedAt, CompletedAt, CreatedAt                                                time.Time
	CheckSuite                                                                       struct {
		App         struct{ Name, Slug string }
		CreatedAt   time.Time
		WorkflowRun struct{ Workflow struct{ Name string } }
	}
}
type contexts struct {
	Nodes    []checkNode
	PageInfo struct {
		HasNextPage bool
		EndCursor   string
	}
}
type gqlPR struct {
	Title, State, HeadRefOID                            string
	MergedAt, ClosedAt                                  time.Time
	Merged, Closed                                      bool
	HeadRefName, BaseRefName, ReviewDecision, Mergeable string
	Author                                              struct{ Login string }
	IsDraft                                             bool
	Additions, Deletions, ChangedFiles                  int
	CreatedAt, UpdatedAt                                time.Time
	Comments                                            struct{ TotalCount int }
	Commits                                             struct {
		TotalCount int
		Nodes      []struct {
			Commit struct{ StatusCheckRollup *struct{ Contexts contexts } }
		}
	}
}

func (g *GitHub) Fetch(ctx context.Context, ref core.Ref) core.PR {
	p := core.PR{Ref: ref}
	// Each fetch gets its own recorder, including Actions detail calls. No credentials
	// are in gh arguments: authentication remains managed by gh.
	traced := *g
	traced.Runner = requestRecorder{Runner: g.Runner, requests: &p.GitHubRequests}
	g = &traced
	cursor := ""
	var nodes []checkNode
	for page := 0; ; page++ {
		if page >= 100 {
			p.Error = "too many pages of checks"
			return p
		}
		args := githubArgs(ref, cursor)
		b, err := g.run(ctx, args...)
		if err != nil {
			p.Error = err.Error()
			return p
		}
		var data struct {
			Data   struct{ Repository struct{ PullRequest *gqlPR } }
			Errors []struct{ Message string }
		}
		if err = json.Unmarshal(b, &data); err != nil {
			p.Error = "invalid GitHub response"
			return p
		}
		if len(data.Errors) > 0 {
			p.Error = "GitHub query failed: " + core.Clean(data.Errors[0].Message)
			return p
		}
		raw := data.Data.Repository.PullRequest
		if raw == nil || raw.HeadRefOID == "" {
			p.Error = "PR not found or inaccessible"
			return p
		}
		if page > 0 && p.Head != raw.HeadRefOID {
			p.Error = "PR changed while reading checks; retrying next refresh"
			return p
		}
		p.Title, p.State, p.Head = core.Clean(raw.Title), raw.State, raw.HeadRefOID
		// Merge is also a closure; merge facts take precedence over a stale state enum.
		if raw.Merged || !raw.MergedAt.IsZero() {
			p.State = "MERGED"
		} else if raw.Closed {
			p.State = "CLOSED"
		}
		if p.State != "OPEN" && p.State != "CLOSED" && p.State != "MERGED" {
			p.Error = "invalid GitHub PR state"
			return p
		}
		p.LastSuccess = time.Now()
		p.Details = core.PRDetails{
			Branch: core.Clean(raw.HeadRefName), BaseBranch: core.Clean(raw.BaseRefName), Author: core.Clean(raw.Author.Login),
			Draft: raw.IsDraft, ReviewDecision: raw.ReviewDecision, Mergeable: raw.Mergeable,
			Additions: raw.Additions, Deletions: raw.Deletions, ChangedFiles: raw.ChangedFiles,
			Commits: raw.Commits.TotalCount, Comments: raw.Comments.TotalCount, CreatedAt: raw.CreatedAt, UpdatedAt: raw.UpdatedAt,
			MergedAt: raw.MergedAt, ClosedAt: raw.ClosedAt,
		}
		if len(raw.Commits.Nodes) == 0 || raw.Commits.Nodes[0].Commit.StatusCheckRollup == nil {
			break
		}
		checks := raw.Commits.Nodes[0].Commit.StatusCheckRollup.Contexts
		nodes = append(nodes, checks.Nodes...)
		if !checks.PageInfo.HasNextPage {
			break
		}
		if checks.PageInfo.EndCursor == "" || checks.PageInfo.EndCursor == cursor {
			p.Error = "invalid GitHub pagination"
			return p
		}
		cursor = checks.PageInfo.EndCursor
	}
	// Deduplicate reruns by logical check, retaining the newest record.
	p.History = map[string]core.Job{}
	latest := map[string]checkNode{}
	var order []string
	for _, n := range nodes {
		observed := jobFromNode(n, p.Head)
		if !g.Jenkins.Match(observed) && observed.RunID != "" {
			p.History[observed.Key+"\x00"+observed.RunID] = observed
		}
		name := n.Name
		if name == "" {
			name = n.Context
		}
		key := n.Type + "/" + n.CheckSuite.App.Slug + "/" + n.CheckSuite.WorkflowRun.Workflow.Name + "/" + name
		prev, exists := latest[key]
		at := n.StartedAt
		if at.IsZero() {
			at = n.CreatedAt
		}
		if at.IsZero() {
			at = n.CheckSuite.CreatedAt
		}
		old := prev.StartedAt
		if old.IsZero() {
			old = prev.CreatedAt
		}
		if old.IsZero() {
			old = prev.CheckSuite.CreatedAt
		}
		if !exists {
			order = append(order, key)
		}
		newer := !at.Before(old)
		// A queued rerun may have no start time yet. Within the same suite,
		// never let an older completed run hide that pending work.
		if exists && n.Type == "CheckRun" && n.CheckSuite.CreatedAt.Equal(prev.CheckSuite.CreatedAt) {
			if n.StartedAt.IsZero() && n.Status != "COMPLETED" && prev.Status == "COMPLETED" {
				newer = true
			}
			if prev.StartedAt.IsZero() && prev.Status != "COMPLETED" && n.Status == "COMPLETED" {
				newer = false
			}
		}
		if !exists || newer {
			latest[key] = n
		}
	}
	jenkinsSeen := map[string]bool{}
	runNumbers := map[string]string{}
	for _, key := range order {
		n := latest[key]
		j := jobFromNode(n, p.Head)
		if issue := core.BuildURLWarning(j.URL); issue != "" {
			j.Warning = issue
		} else if g.Jenkins.Match(j) {
			j.Provider = "Jenkins"
			// Multiple GitHub contexts may report the same Jenkins build.
			canonical := j.URL
			if root, err := jenkinsBuildRoot(j.URL); err == nil {
				canonical = root
			}
			if jenkinsSeen[canonical] {
				continue
			}
			jenkinsSeen[canonical] = true
			j = g.Jenkins.Enrich(ctx, j)
		} else if n.CheckSuite.App.Slug == "github-actions" {
			j = g.enrichActions(ctx, ref, j, runNumbers)
		}
		p.Jobs = append(p.Jobs, j)
	}
	return p
}

func jobFromNode(n checkNode, head string) core.Job {
	j := core.Job{Name: core.Clean(n.Name), Provider: core.Clean(n.CheckSuite.App.Name), URL: n.DetailsURL, RunID: n.ID, StartedAt: n.StartedAt, CompletedAt: n.CompletedAt, Progress: -1}
	if name := n.CheckSuite.WorkflowRun.Workflow.Name; name != "" {
		j.Name = core.Clean(name) + " / " + j.Name
	}
	if n.Type == "StatusContext" {
		j.Name = core.Clean(n.Context)
		j.URL = n.TargetURL
		j.Status = normalize(n.State)
		j.Phase = core.Clean(n.Description)
		// StatusContext IDs change as the same build posts status updates.
		j.RunID = j.URL
		if j.RunID == "" {
			j.RunID = head + "/" + n.Context
		}
	} else {
		j.Status = normalize(n.Status)
		if n.Status == "COMPLETED" {
			j.Status = normalize(n.Conclusion)
		}
	}
	if j.Provider == "" {
		j.Provider = inferProvider(j.URL, j.Name)
	}
	if j.Phase == "" {
		j.Phase = j.Status
	}
	if Terminal := core.Terminal(j.Status); Terminal {
		j.Progress = 1
	}
	if !j.StartedAt.IsZero() && !j.CompletedAt.IsZero() {
		j.Duration = j.CompletedAt.Sub(j.StartedAt)
	}
	j.Key = j.Provider + "/" + j.Name
	return j
}
func normalize(s string) string {
	switch strings.ToUpper(s) {
	case "SUCCESS":
		return "passed"
	case "FAILURE", "ERROR", "TIMED_OUT", "ACTION_REQUIRED", "STARTUP_FAILURE":
		return "failed"
	case "CANCELLED", "ABORTED", "NOT_BUILT":
		return "cancelled"
	case "NEUTRAL":
		return "neutral"
	case "SKIPPED":
		return "skipped"
	case "UNSTABLE":
		return "unstable"
	case "IN_PROGRESS":
		return "running"
	case "PENDING", "QUEUED", "WAITING", "REQUESTED", "EXPECTED":
		return "queued"
	default:
		return "unknown"
	}
}
func inferProvider(raw, name string) string {
	u, _ := url.Parse(raw)
	host := ""
	if u != nil {
		host = strings.ToLower(u.Hostname())
	}
	switch {
	case strings.Contains(strings.ToLower(name), "jenkins") || strings.Contains(host, "jenkins"):
		return "Jenkins"
	case strings.Contains(host, "circleci"):
		return "CircleCI"
	case strings.Contains(host, "buildkite"):
		return "Buildkite"
	case strings.Contains(host, "travis"):
		return "Travis CI"
	case strings.Contains(host, "dev.azure.com"):
		return "Azure Pipelines"
	case strings.Contains(host, "gitlab"):
		return "GitLab CI"
	case host != "":
		return host
	default:
		return "Checks"
	}
}

func (g *GitHub) Discover(ctx context.Context) ([]core.Ref, error) {
	// Search API supports enterprise hosts and authenticates as the current gh user.
	b, err := g.run(ctx, "api", "--hostname", g.Config.GitHubHost, "user")
	if err != nil {
		return nil, err
	}
	var user struct{ Login string }
	if json.Unmarshal(b, &user) != nil || user.Login == "" {
		return nil, fmt.Errorf("could not determine GitHub user")
	}
	var refs []core.Ref
	seen := map[string]bool{}
	now := time.Now()
	window := g.Config.RetentionDuration()
	if window < 0 {
		window = 24 * time.Hour
	}
	queries := []string{"is:pr is:open author:" + user.Login}
	if window > 0 {
		queries = append(queries, "is:pr is:closed author:"+user.Login+" closed:>="+now.Add(-window).UTC().Format("2006-01-02"))
	}
	for queryIndex, search := range queries {
		for page := 1; page <= 10 && len(refs) < g.Config.DiscoveryLimit; page++ {
			b, err = g.run(ctx, "api", "--hostname", g.Config.GitHubHost, "--method", "GET", "search/issues", "-f", "q="+search, "-f", "sort=updated", "-f", "order=desc", "-F", "per_page=100", "-F", "page="+strconv.Itoa(page))
			if err != nil {
				return nil, err
			}
			var result struct {
				IncompleteResults bool `json:"incomplete_results"`
				Items             []struct {
					ClosedAt    time.Time `json:"closed_at"`
					PullRequest struct {
						HTMLURL string `json:"html_url"`
					} `json:"pull_request"`
				}
			}
			if json.Unmarshal(b, &result) != nil {
				return nil, fmt.Errorf("invalid discovery response")
			}
			if result.IncompleteResults {
				return nil, fmt.Errorf("GitHub returned incomplete search results; retry discovery")
			}
			for _, item := range result.Items {
				if queryIndex > 0 && !item.ClosedAt.IsZero() && !item.ClosedAt.After(now.Add(-window)) {
					continue
				}
				r, e := core.ParseRef(item.PullRequest.HTMLURL, g.Config.GitHubHost)
				if e == nil && !seen[strings.ToLower(r.URL)] {
					seen[strings.ToLower(r.URL)] = true
					refs = append(refs, r)
				}
				if len(refs) >= g.Config.DiscoveryLimit {
					break
				}
			}
			if len(result.Items) < 100 {
				break
			}
		}
	}
	return refs, nil
}

func (g *GitHub) enrichActions(ctx context.Context, ref core.Ref, j core.Job, numbers map[string]string) core.Job {
	u, e := url.Parse(j.URL)
	if e != nil || !strings.EqualFold(u.Host, ref.Host) {
		return j
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) != 7 || parts[2] != "actions" || parts[3] != "runs" || parts[5] != "job" {
		return j
	}
	runID, jobID := parts[4], parts[6]
	if _, e = strconv.ParseUint(jobID, 10, 64); e != nil {
		return j
	}
	if _, e = strconv.ParseUint(runID, 10, 64); e != nil {
		return j
	}
	b, err := g.run(ctx, "api", "--hostname", ref.Host, "repos/"+ref.Repo+"/actions/jobs/"+jobID)
	if err != nil {
		j.Warning = "Actions steps unavailable"
		return j
	}
	var job struct {
		RunAttempt         int `json:"run_attempt"`
		Status, Conclusion string
		StartedAt          time.Time `json:"started_at"`
		CompletedAt        time.Time `json:"completed_at"`
		Steps              []struct{ Name, Status, Conclusion string }
	}
	if json.Unmarshal(b, &job) != nil || job.Status == "" {
		j.Warning = "invalid Actions job response"
		return j
	}
	j.Status = normalize(job.Status)
	if job.Status == "completed" {
		j.Status = normalize(job.Conclusion)
	}
	j.StartedAt, j.CompletedAt = job.StartedAt, job.CompletedAt
	j.Phase = j.Status
	complete := 0
	active := []string{}
	for _, step := range job.Steps {
		if step.Status == "completed" {
			complete++
		}
		if step.Status == "in_progress" {
			active = append(active, core.Clean(step.Name))
		}
	}
	if len(active) > 0 {
		j.Phase = strings.Join(active, " + ")
	}
	if core.Terminal(j.Status) {
		j.Progress = 1
	} else if len(job.Steps) > 0 {
		j.Progress = min(.99, float64(complete)/float64(len(job.Steps)))
	}
	if !j.StartedAt.IsZero() && !j.CompletedAt.IsZero() {
		j.Duration = j.CompletedAt.Sub(j.StartedAt)
	}
	j.Number = runID
	if n, ok := numbers[runID]; ok {
		j.Number = n
	} else {
		b, err = g.run(ctx, "api", "--hostname", ref.Host, "repos/"+ref.Repo+"/actions/runs/"+runID)
		var run struct {
			RunNumber int `json:"run_number"`
		}
		if err == nil && json.Unmarshal(b, &run) == nil && run.RunNumber > 0 {
			j.Number = strconv.Itoa(run.RunNumber)
		}
		numbers[runID] = j.Number
	}
	if job.RunAttempt > 1 {
		j.Number += fmt.Sprintf(".%d", job.RunAttempt)
	}
	return j
}
