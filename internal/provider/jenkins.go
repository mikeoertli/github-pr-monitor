package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mikeoertli/github-pr-monitor/internal/config"
	"github.com/mikeoertli/github-pr-monitor/internal/core"
)

var buildPath = regexp.MustCompile(`/job/.+/([0-9]+)/?$`)

type Jenkins struct {
	Config    config.Config
	Client    *http.Client
	mu        sync.Mutex
	estimates map[string]int64
}

func NewJenkins(c config.Config) *Jenkins {
	return &Jenkins{Config: c, Client: &http.Client{Timeout: c.RequestTimeout()}, estimates: map[string]int64{}}
}
func under(raw, base string) bool {
	u, e := url.Parse(raw)
	if e != nil {
		return false
	}
	b, e := url.Parse(base)
	if e != nil {
		return false
	}
	return strings.EqualFold(u.Scheme, b.Scheme) && strings.EqualFold(u.Host, b.Host) && (u.Path == strings.TrimRight(b.Path, "/") || strings.HasPrefix(u.Path, strings.TrimRight(b.Path, "/")+"/"))
}
func (j *Jenkins) Match(job core.Job) bool {
	_, e := url.Parse(job.URL)
	if e != nil {
		return false
	}
	if _, err := jenkinsBuildRoot(job.URL); err == nil || job.Provider == "Jenkins" {
		return true
	}
	for _, server := range j.Config.Jenkins {
		if under(job.URL, server.URL) {
			return true
		}
	}
	return false
}

type httpError int

func (e httpError) Error() string { return fmt.Sprintf("Jenkins HTTP %d", int(e)) }
func (j *Jenkins) get(ctx context.Context, raw string, out any) error {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.User != nil || (u.Scheme != "https" && u.Scheme != "http") {
		return fmt.Errorf("invalid Jenkins API URL")
	}
	ctx, cancel := context.WithTimeout(ctx, j.Config.RequestTimeout())
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		return fmt.Errorf("invalid Jenkins request")
	}
	req.Header.Set("Accept", "application/json")
	authRoot := ""
	if server := jenkinsServer(j.Config, raw); server != nil {
		user, token := server.User, server.Token
		if v := os.Getenv(server.UserEnv); v != "" {
			user = v
		}
		if v := os.Getenv(server.TokenEnv); v != "" {
			token = v
		}
		if user != "" || token != "" {
			req.SetBasicAuth(user, token)
			authRoot = server.URL
		}
	}

	client := *j.Client
	client.CheckRedirect = func(next *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return fmt.Errorf("too many redirects")
		}
		if !strings.EqualFold(next.URL.Host, u.Host) || !strings.EqualFold(next.URL.Scheme, u.Scheme) {
			return fmt.Errorf("cross-origin Jenkins redirect blocked")
		}
		if authRoot != "" && !under(next.URL.String(), authRoot) {
			return fmt.Errorf("Jenkins redirect outside configured path blocked")
		}
		return nil
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("Jenkins request failed (check server, credentials, and connectivity)")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return httpError(resp.StatusCode)
	}
	if err = json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(out); err != nil {
		return fmt.Errorf("invalid Jenkins JSON response")
	}
	return nil
}

type build struct {
	Number                                 int
	Building                               *bool
	Result                                 *string
	Timestamp, Duration, EstimatedDuration int64
	NextBuild                              *struct{ URL string }
}

func (j *Jenkins) estimate(ctx context.Context, raw string, b build) int64 {
	j.mu.Lock()
	n, ok := j.estimates[raw]
	j.mu.Unlock()
	if ok {
		return n
	}
	var previous build
	err := j.get(ctx, estimateURL(raw), &previous)
	n = b.EstimatedDuration
	if err == nil && previous.Duration > 0 {
		n = previous.Duration
	}
	if n > 0 {
		j.mu.Lock()
		j.estimates[raw] = n
		j.mu.Unlock()
	}
	return n
}

func (j *Jenkins) Enrich(ctx context.Context, job core.Job) core.Job {
	raw, e := jenkinsBuildRoot(job.URL)
	if e != nil {
		job.Warning = "Jenkins URL must identify a numbered build or a report within one; using GitHub status"
		return job
	}
	job.JenkinsRequests = nil
	get := func(raw string, out any) error {
		job.JenkinsRequests = append(job.JenkinsRequests, raw)
		return j.get(ctx, raw, out)
	}
	job.Key = "Jenkins/" + raw[:strings.LastIndex(raw, "/")]
	job.RunID = raw
	job.Number = raw[strings.LastIndex(raw, "/")+1:]
	var b build
	for hop := 0; hop < 8; hop++ {
		job.URL, job.RunID, job.Number = raw+"/", raw, raw[strings.LastIndex(raw, "/")+1:]
		if err := get(raw+"/api/json", &b); err != nil {
			job.Warning = err.Error()
			job.Stale = true
			return job
		}
		if b.Building == nil || b.Number < 1 || (!*b.Building && b.Result == nil) {
			job.Warning = "incomplete Jenkins build response"
			job.Stale = true
			return job
		}
		if !*b.Building && b.Result != nil && (*b.Result == "ABORTED" || *b.Result == "NOT_BUILT") && b.NextBuild != nil {
			next := strings.TrimRight(b.NextBuild.URL, "/")
			parent := raw[:strings.LastIndex(raw, "/")]
			nextURL, err := url.Parse(next)
			if err == nil && under(next, parent) && buildPath.MatchString(nextURL.Path) && next != raw && next[:strings.LastIndex(next, "/")] == parent {
				if hop == 7 {
					job.Warning = "too many superseding Jenkins builds"
					job.Stale = true
					return job
				}
				previous := job
				previous.PreviousRuns = nil
				previous.Key = "Jenkins/" + parent
				previous.RunID, previous.URL, previous.Number = raw, raw+"/", strconv.Itoa(b.Number)
				previous.Status, previous.Phase, previous.Progress = normalize(*b.Result), "superseded", 1
				previous.StartedAt = time.UnixMilli(b.Timestamp)
				previous.Duration = time.Duration(b.Duration) * time.Millisecond
				previous.CompletedAt = previous.StartedAt.Add(previous.Duration)
				job.PreviousRuns = append(job.PreviousRuns, previous)
				raw = next
				b = build{}
				continue
			}
		}
		break
	}
	job.URL = raw + "/"
	job.Provider = "Jenkins"
	job.Number = strconv.Itoa(b.Number)
	job.RunID = raw
	job.Key = "Jenkins/" + raw[:strings.LastIndex(raw, "/")]
	job.StartedAt = time.UnixMilli(b.Timestamp)
	job.Duration = time.Duration(b.Duration) * time.Millisecond
	job.Estimated = false
	if *b.Building {
		job.Status = "running"
		job.Phase = "Building"
		job.Progress = -1
		job.JenkinsRequests = append(job.JenkinsRequests, estimateURL(raw))
		estimate := j.estimate(ctx, raw, b)
		if estimate > 0 && b.Timestamp > 0 {
			elapsed := max(int64(0), time.Now().UnixMilli()-b.Timestamp)
			job.Progress = min(.99, float64(elapsed)/float64(estimate))
			job.Estimated = true
			job.ExpectedDuration = time.Duration(estimate) * time.Millisecond
			if elapsed > estimate {
				job.Phase = "Building · +" + core.Duration(time.Duration(elapsed-estimate)*time.Millisecond) + " over estimate"
			}
		}
	} else {
		job.Status = normalize(*b.Result)
		job.Phase = job.Status
		job.Progress = 1
		job.CompletedAt = job.StartedAt.Add(job.Duration)
	}
	var pipeline struct {
		Stages []struct{ Name, Status string }
	}
	err := get(raw+"/wfapi/describe", &pipeline)
	if err == nil {
		var active []string
		for _, stage := range pipeline.Stages {
			if stage.Status == "IN_PROGRESS" || stage.Status == "PAUSED_PENDING_INPUT" {
				active = append(active, core.Clean(stage.Name))
			}
		}
		if len(active) > 0 {
			phase := strings.Join(active, " + ")
			if strings.Contains(job.Phase, "over estimate") {
				phase += " · " + strings.SplitN(job.Phase, " · ", 2)[1]
			}
			job.Phase = phase
		}
	} else if err != httpError(404) && err != httpError(400) {
		job.Warning = "Jenkins stages unavailable; build status is current"
	}
	return job
}
