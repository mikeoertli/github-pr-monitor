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
	Version int
	PRs     []PR
}

func LoadSession(path string) ([]PR, error) {
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var s Session
	if err = json.Unmarshal(b, &s); err != nil {
		return nil, fmt.Errorf("cannot restore session: %w", err)
	}
	if s.Version != 1 {
		return nil, fmt.Errorf("unsupported session version %d", s.Version)
	}
	var prs []PR
	for _, p := range s.PRs {
		ref, err := ParseRef(p.Ref.URL, p.Ref.Host)
		if err != nil {
			return nil, fmt.Errorf("invalid saved PR: %w", err)
		}
		p.Ref = ref
		p.Fresh = false
		if !p.Removed {
			prs = append(prs, p)
		}
	}
	return prs, nil
}
func SaveSession(path string, prs []PR) error {
	b, err := json.MarshalIndent(Session{Version: 1, PRs: prs}, "", "  ")
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
		if p.Removed {
			status += " (removed from monitor)"
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
