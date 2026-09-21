// Package core contains provider-independent monitoring and lifecycle rules.
package core

import (
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
)

type Ref struct {
	URL, Host, Repo string
	Number          int
}

var shortRef = regexp.MustCompile(`^([A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+)#([0-9]+)$`)

func ParseRef(raw, host string) (Ref, error) {
	raw = strings.TrimSpace(raw)
	if m := shortRef.FindStringSubmatch(raw); m != nil {
		raw = "https://" + host + "/" + m[1] + "/pull/" + m[2]
	}
	u, e := url.Parse(raw)
	if e != nil || u.Scheme != "https" || u.Host == "" || u.User != nil {
		return Ref{}, fmt.Errorf("expected a PR URL or owner/repo#123: %q", raw)
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) < 4 || parts[2] != "pull" || !regexp.MustCompile(`^[A-Za-z0-9_.-]+$`).MatchString(parts[0]) || !regexp.MustCompile(`^[A-Za-z0-9_.-]+$`).MatchString(parts[1]) {
		return Ref{}, fmt.Errorf("not a PR URL: %q", raw)
	}
	n, e := strconv.Atoi(parts[3])
	if e != nil || n <= 0 {
		return Ref{}, fmt.Errorf("invalid PR number: %q", raw)
	}
	repo := parts[0] + "/" + parts[1]
	return Ref{URL: fmt.Sprintf("https://%s/%s/pull/%d", strings.ToLower(u.Host), repo, n), Host: strings.ToLower(u.Host), Repo: repo, Number: n}, nil
}

var pastedRefs = regexp.MustCompile(`https://[^\s/<>"\x60]+/[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+/pull/[0-9]+|[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+#[0-9]+`)

func ParseBatch(text, host string) ([]Ref, error) {
	var refs []Ref
	seen := map[string]bool{}
	for _, raw := range pastedRefs.FindAllString(text, -1) {
		raw = strings.TrimRight(raw, ".,;:)]}")
		r, e := ParseRef(raw, host)
		if e != nil {
			continue
		}
		if !seen[strings.ToLower(r.URL)] {
			refs = append(refs, r)
			seen[strings.ToLower(r.URL)] = true
		}
	}
	if len(refs) == 0 {
		return nil, fmt.Errorf("no PR links found; paste https://github.com/owner/repo/pull/123 or owner/repo#123")
	}
	return refs, nil
}

// Clean makes remote/user-controlled text safe to render in a terminal.
func Clean(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || r == 0x7f {
			return ' '
		}
		return r
	}, s)
}

type Job struct {
	JenkinsRequests             []string `json:",omitempty"` // API sources for this snapshot; never credentials.
	Key                         string   // Stable logical job identity, across reruns.
	RunID                       string   // Distinct observed run identity; status changes do not increment it.
	Name, Provider, URL, Number string
	Status, Phase               string
	Progress                    float64 // -1 = unknown; estimates never reach 1 until terminal.
	Estimated                   bool
	ExpectedDuration            time.Duration // Reference runtime; separate from completion percentage.
	StartedAt, CompletedAt      time.Time
	Duration                    time.Duration
	Monitored                   time.Duration
	Stale                       bool
	Warning                     string
	PreviousRuns                []Job `json:"-"`
}

func Terminal(s string) bool {
	switch s {
	case "passed", "failed", "cancelled", "skipped", "neutral", "unstable":
		return true
	}
	return false
}
func Passing(s string) bool { return s == "passed" || s == "skipped" || s == "neutral" }

// BuildURLWarning checks links without probing arbitrary third-party websites.
func BuildURLWarning(raw string) string {
	if raw == "" {
		return "CI did not report a build URL"
	}
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" || u.User != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return "CI reported an invalid HTTP(S) build URL"
	}
	return ""
}

func (p PR) Warnings() []string {
	var warnings []string
	if p.Error != "" {
		warnings = append(warnings, p.Error)
	}
	for _, j := range p.Jobs {
		issue := j.Warning
		if issue == "" {
			issue = BuildURLWarning(j.URL)
		}
		if issue != "" {
			warnings = append(warnings, j.Name+": "+issue)
		}
	}
	return warnings
}

// PRDetails describes the latest GitHub metadata for the monitored head.
type PRDetails struct {
	Branch, BaseBranch, Author                            string
	Draft                                                 bool
	ReviewDecision, Mergeable                             string
	Additions, Deletions, ChangedFiles, Commits, Comments int
	CreatedAt, UpdatedAt                                  time.Time
	MergedAt, ClosedAt                                    time.Time
}

type PR struct {
	ClosedObservedAt   time.Time  // Fallback retention clock if GitHub omits the closure timestamp.
	Expired            bool       // Removed by retention rather than a manual dismissal.
	GitHubRequests     [][]string `json:",omitempty"` // Exact gh API arguments for the most recent update.
	Details            PRDetails
	LastAttempt        time.Time
	Ref                Ref
	Title, Head, State string
	Jobs               []Job
	History            map[string]Job
	FirstSeen          time.Time
	Monitored          time.Duration
	LastSuccess        time.Time
	Error              string
	Fresh              bool `json:"-"`
	MergeNote          string
	Removed            bool
}

func NewPR(ref Ref, now time.Time) PR {
	return PR{Ref: ref, State: "UNKNOWN", FirstSeen: now, History: map[string]Job{}}
}
func (p *PR) Apply(next PR, now time.Time) {
	p.LastAttempt = now
	if len(next.GitHubRequests) > 0 {
		p.GitHubRequests = next.GitHubRequests
	}
	if next.Error == "" && !p.Details.UpdatedAt.IsZero() && !next.Details.UpdatedAt.IsZero() && next.Details.UpdatedAt.Before(p.Details.UpdatedAt) {
		next.Error = "GitHub returned older PR data; retaining the last successful snapshot"
	}
	if next.Error == "" && p.State == "MERGED" && next.State != "MERGED" {
		next.Error = "GitHub returned an inconsistent state for a merged PR; retaining the last successful snapshot"
	}
	if next.Error != "" {
		p.Error = next.Error
		p.Fresh = false
		return
	}
	// A merge note describes checks at the observed transition, never a later rerun.
	if p.State != "MERGED" && next.State == "MERGED" && !p.LastSuccess.IsZero() && p.Head == next.Head {
		failed, pending := false, false
		for _, j := range next.Jobs {
			if !Terminal(j.Status) {
				pending = true
			} else if !Passing(j.Status) {
				failed = true
			}
		}
		switch {
		case failed && pending:
			p.MergeNote = "merged with failing/pending checks"
		case failed:
			p.MergeNote = "merged with failing checks"
		case pending:
			p.MergeNote = "merged with pending checks"
		}
	}
	if next.Closed() {
		if !p.Closed() || p.ClosedObservedAt.IsZero() {
			p.ClosedObservedAt = now
		}
	} else {
		p.ClosedObservedAt = time.Time{}
	}
	p.Title, p.Head, p.State, p.Jobs = next.Title, next.Head, next.State, next.Jobs
	p.Details = next.Details
	p.Error = ""
	p.Fresh = true
	p.LastSuccess = next.LastSuccess
	if p.LastSuccess.IsZero() {
		p.LastSuccess = now
	}
	if p.History == nil {
		p.History = map[string]Job{}
	}
	record := func(j Job) {
		if j.RunID != "" {
			key := j.Key + "\x00" + j.RunID
			j.Monitored = p.History[key].Monitored
			j.PreviousRuns = nil
			p.History[key] = j
		}
	}
	for _, j := range next.History {
		record(j)
	}
	for _, j := range next.Jobs {
		for _, previous := range j.PreviousRuns {
			record(previous)
		}
		record(j)
	}
}
func (p PR) Closed() bool { return p.State == "MERGED" || p.State == "CLOSED" }
func (p PR) CompletionTime() time.Time {
	if p.State == "MERGED" && !p.Details.MergedAt.IsZero() {
		return p.Details.MergedAt
	}
	if p.State == "CLOSED" && !p.Details.ClosedAt.IsZero() {
		return p.Details.ClosedAt
	}
	return p.ClosedObservedAt
}
func (p PR) RetentionExpired(now time.Time, retention time.Duration) bool {
	at := p.CompletionTime()
	return !p.Removed && p.Fresh && p.Error == "" && p.Closed() && retention >= 0 && !at.IsZero() && !now.Before(at.Add(retention))
}

func (p PR) Status() string {
	if p.Error != "" {
		return "stale"
	}
	if len(p.Jobs) == 0 {
		return "no checks"
	}
	pending, failed := false, false
	for _, j := range p.Jobs {
		if j.Stale {
			return "stale"
		}
		if !Terminal(j.Status) {
			pending = true
		}
		if Terminal(j.Status) && !Passing(j.Status) {
			failed = true
		}
	}
	if pending {
		return "building"
	}
	if failed {
		return "failed"
	}
	return "passed"
}
func (p PR) Progress() float64 {
	if len(p.Jobs) == 0 {
		return -1
	}
	sum := 0.0
	for _, j := range p.Jobs {
		if j.Progress < 0 {
			return -1
		}
		sum += j.Progress
	}
	return sum / float64(len(p.Jobs))
}
func (p PR) FinalStatus() string {
	if p.MergeNote != "" && p.State == "MERGED" {
		return p.MergeNote
	}
	return strings.ToLower(p.State)
}

// ShouldQuit examines every PR supplied by the caller's monitoring scope.
// No-check and stale snapshots must not trigger build-related exits.
func ShouldQuit(prs []PR, mode string) bool {
	if mode == "never" {
		return false
	}
	n := 0
	for _, p := range prs {
		if p.Removed {
			continue
		}
		n++
		if !p.Fresh || p.Error != "" {
			return false
		}
		if mode == "all-closed" {
			if p.State != "MERGED" && p.State != "CLOSED" {
				return false
			}
			continue
		}
		if mode != "builds-finished" && mode != "all-passing" {
			return false
		}
		if len(p.Jobs) == 0 {
			return false
		}
		for _, j := range p.Jobs {
			if j.Stale || !Terminal(j.Status) {
				return false
			}
			if mode == "all-passing" && !Passing(j.Status) {
				return false
			}
		}
	}
	return n > 0
}

func Fuzzy(query, text string) bool {
	hay := []rune(strings.ToLower(text))
	pos := 0
	for _, r := range strings.ToLower(query) {
		if unicode.IsSpace(r) {
			continue
		}
		found := false
		for pos < len(hay) {
			c := hay[pos]
			pos++
			if c == r {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func Sorted(prs []PR, filter, by string, desc bool) []int {
	var result []int
	for i, p := range prs {
		if p.Removed {
			continue
		}
		fields := []string{p.Ref.Repo, fmt.Sprintf("#%d", p.Ref.Number), p.Title, p.State, p.Status(), p.Details.Branch, p.Details.BaseBranch, p.Details.Author}
		for _, j := range p.Jobs {
			fields = append(fields, j.Name, j.Provider, j.Phase)
		}
		matches := true
		for _, term := range strings.Fields(filter) {
			found := false
			for _, field := range fields {
				if Fuzzy(term, field) {
					found = true
					break
				}
			}
			if !found {
				matches = false
				break
			}
		}
		if matches {
			result = append(result, i)
		}
	}
	sort.SliceStable(result, func(i, j int) bool {
		a, b := prs[result[i]], prs[result[j]]
		cmp := 0
		if by == "progress" && a.Progress() != b.Progress() {
			if a.Progress() < b.Progress() {
				cmp = -1
			} else {
				cmp = 1
			}
		} else {
			cmp = strings.Compare(strings.ToLower(a.Ref.Repo), strings.ToLower(b.Ref.Repo))
			if cmp == 0 {
				cmp = a.Ref.Number - b.Ref.Number
			}
		}
		if desc {
			return cmp > 0
		}
		return cmp < 0
	})
	return result
}
func Duration(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	return d.Round(time.Second).String()
}
