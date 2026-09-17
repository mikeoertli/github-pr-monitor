package core

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"
	"time"
)

type Session struct {
	Version   int
	PRs       []PR
	Dismissed []Ref `json:",omitempty"`
}

func LoadSession(path string) ([]PR, error) {
	s, err := LoadSessionState(path)
	return s.PRs, err
}

func LoadSessionState(path string) (Session, error) {
	empty := Session{Version: 1}
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return empty, nil
	}
	if err != nil {
		return empty, err
	}
	var s Session
	if err = json.Unmarshal(b, &s); err != nil {
		return empty, fmt.Errorf("cannot restore session: %w", err)
	}
	if s.Version != 1 {
		return empty, fmt.Errorf("unsupported session version %d", s.Version)
	}
	var prs []PR
	dismissed := map[string]Ref{}
	for _, ref := range s.Dismissed {
		r, err := ParseRef(ref.URL, ref.Host)
		if err != nil {
			return empty, fmt.Errorf("invalid saved dismissal: %w", err)
		}
		dismissed[strings.ToLower(r.URL)] = r
	}
	for _, p := range s.PRs {
		ref, err := ParseRef(p.Ref.URL, p.Ref.Host)
		if err != nil {
			return empty, fmt.Errorf("invalid saved PR: %w", err)
		}
		p.Ref, p.Fresh = ref, false
		if !p.Removed {
			prs = append(prs, p)
		} else if !p.Expired {
			dismissed[strings.ToLower(ref.URL)] = ref
		}
	}
	s.PRs, s.Dismissed = prs, nil
	for _, ref := range dismissed {
		s.Dismissed = append(s.Dismissed, ref)
	}
	return s, nil
}
func SaveSession(path string, prs []PR, dismissed ...Ref) error {
	byURL := map[string]Ref{}
	for _, ref := range dismissed {
		byURL[strings.ToLower(ref.URL)] = ref
	}
	for _, p := range prs {
		if p.Removed && !p.Expired {
			byURL[strings.ToLower(p.Ref.URL)] = p.Ref
		} else if !p.Removed {
			delete(byURL, strings.ToLower(p.Ref.URL))
		}
	}
	dismissed = nil
	for _, ref := range byURL {
		dismissed = append(dismissed, ref)
	}
	sort.Slice(dismissed, func(i, j int) bool { return dismissed[i].URL < dismissed[j].URL })
	b, err := json.MarshalIndent(Session{Version: 1, PRs: prs, Dismissed: dismissed}, "", "  ")
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".session-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(b); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}

func Summary(prs []PR, elapsed time.Duration) string {
	var b strings.Builder
	fmt.Fprintf(&b, "gprm · session %s\n", Duration(elapsed))
	w := tabwriter.NewWriter(&b, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "PR / JOB\tOBSERVED RUNS\tMONITORED\tFINAL STATUS")
	for _, p := range prs {
		status := p.FinalStatus()
		if p.Expired {
			status += " (retention expired)"
		} else if p.Removed {
			status += " (dismissed)"
		}
		if p.Error != "" || !p.Fresh {
			status += " · stale"
		}
		fmt.Fprintf(w, "%s #%d\t%d\t%s\t%s · %s\n", Clean(p.Ref.Repo), p.Ref.Number, len(p.History), Duration(p.Monitored), Clean(status), p.Status())
		groups := map[string][]Job{}
		for _, j := range p.History {
			groups[j.Key] = append(groups[j.Key], j)
		}
		var keys []string
		for k := range groups {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, key := range keys {
			jobs := groups[key]
			var monitored time.Duration
			for _, j := range jobs {
				monitored += j.Monitored
			}
			sort.SliceStable(jobs, func(i, k int) bool {
				if jobs[i].StartedAt.Equal(jobs[k].StartedAt) {
					return jobs[i].RunID < jobs[k].RunID
				}
				return jobs[i].StartedAt.Before(jobs[k].StartedAt)
			})
			last := jobs[len(jobs)-1]
			// Prefer the current job over timestamp ordering for providers without dates.
			for _, current := range p.Jobs {
				if current.Key == key {
					last = current
					break
				}
			}
			number := last.Number
			if number != "" {
				number = " #" + number
			}
			fmt.Fprintf(w, "  %s / %s%s\t%d\t%s\t%s\n", Clean(last.Provider), Clean(last.Name), Clean(number), len(jobs), Duration(monitored), Clean(last.Status))
		}
	}
	w.Flush()
	if len(prs) == 0 {
		b.WriteString("No PRs monitored.\n")
	}
	b.WriteString("Run counts include distinct runs observed by this monitor; time excludes time while the app was closed.\n")
	return b.String()
}
