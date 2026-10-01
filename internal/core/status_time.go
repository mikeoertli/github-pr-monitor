package core

import (
	"fmt"
	"time"
)

// DisplayStatus keeps PR lifecycle separate from the aggregate CI result.
func (p PR) DisplayStatus() string {
	if p.State == "MERGED" {
		return "merged"
	}
	if p.State == "CLOSED" {
		return "closed"
	}
	return p.Status()
}

// StatusTime uses provider event times when the aggregate transition is known.
// PR updatedAt is deliberately excluded: unrelated edits also change it.
func (p PR) StatusTime() time.Time {
	if p.Closed() {
		if p.State == "MERGED" {
			return p.Details.MergedAt
		}
		return p.Details.ClosedAt
	}
	if p.Error != "" || len(p.Jobs) == 0 {
		return time.Time{}
	}
	var at time.Time
	if p.Status() == "building" {
		for _, j := range p.Jobs {
			if Terminal(j.Status) {
				continue
			}
			if j.StartedAt.IsZero() {
				return time.Time{}
			}
			if at.IsZero() || j.StartedAt.Before(at) {
				at = j.StartedAt
			}
		}
		return at
	}
	if p.Status() == "stale" {
		return time.Time{}
	}
	for _, j := range p.Jobs {
		if j.CompletedAt.IsZero() {
			return time.Time{}
		}
		if j.CompletedAt.After(at) {
			at = j.CompletedAt
		}
	}
	return at
}

func (p *PR) recordStatusTime(previous PR, now time.Time) {
	// Fetch failures are not remote status transitions.
	previous.Error = ""
	previousStatus := previous.StatusChangedStatus
	if previousStatus == "" {
		previousStatus = previous.DisplayStatus()
	}
	if p.DisplayStatus() == "stale" {
		return
	}
	p.StatusChangedStatus = p.DisplayStatus()
	if p.DisplayStatus() == "building" && previousStatus == "building" && previous.Head == p.Head && !p.StatusChangedAt.IsZero() {
		return
	}
	if at := p.StatusTime(); !at.IsZero() {
		p.StatusChangedAt, p.StatusTimeObserved = at, false
		return
	}
	if p.StatusChangedAt.IsZero() || previousStatus != p.DisplayStatus() || previous.Head != p.Head {
		p.StatusChangedAt, p.StatusTimeObserved = now, true
	}
}

// RelativeTime uses whole minutes up to two hours to keep recent events precise.
func RelativeTime(at, now time.Time) string {
	if at.IsZero() {
		return ""
	}
	d := now.Sub(at)
	switch {
	case d < time.Minute:
		return "just now"
	case d < 2*time.Hour:
		return fmt.Sprintf("%dm ago", int(d/time.Minute))
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh ago", int(d/time.Hour))
	default:
		return fmt.Sprintf("%dd ago", int(d/(24*time.Hour)))
	}
}

func (p PR) StatusAge(now time.Time) string {
	age := RelativeTime(p.StatusChangedAt, now)
	if age != "" && p.StatusTimeObserved {
		return "~" + age
	}
	return age
}
