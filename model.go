package main

import (
	"fmt"
	"time"
)

type rateLimitInfo struct {
	Cost      int
	Remaining int
	ResetAt   time.Time
	Valid     bool
}

func mergeRateLimit(current, next rateLimitInfo) rateLimitInfo {
	if !next.Valid {
		return current
	}
	if !current.Valid {
		return next
	}
	out := current
	if next.Remaining < out.Remaining {
		out.Remaining = next.Remaining
	}
	if next.ResetAt.Before(out.ResetAt) {
		out.ResetAt = next.ResetAt
	}
	out.Cost += next.Cost
	return out
}

func chooseSleep(base time.Duration, rl rateLimitInfo, now time.Time) time.Duration {
	if !rl.Valid {
		return base
	}
	if rl.ResetAt.Compare(now) <= 0 {
		return base
	}
	if rl.Remaining <= 0 {
		return rl.ResetAt.Sub(now) + 2*time.Second
	}
	timeLeft := rl.ResetAt.Sub(now)
	budgeted := timeLeft / time.Duration(rl.Remaining)
	if budgeted > base {
		return budgeted
	}
	return base
}

type eventType string

const (
	eventMerged   eventType = "merged"
	eventDequeued eventType = "dequeued"
	eventConflict eventType = "conflict"
	eventChecks   eventType = "checks"
)

type prSnapshot struct {
	ID                string
	Number            int
	Title             string
	URL               string
	Repo              string
	State             string
	Mergeable         string
	MergeStateStatus  string
	HeadOID           string
	HeadRollupState   string
	QueueEntryID      string
	QueueHeadOID      string
	QueueRollupState  string
	QueueEntryPresent bool
}

func (p prSnapshot) ref() string {
	return fmt.Sprintf("%s#%d", p.Repo, p.Number)
}

type event struct {
	Type          eventType
	PR            prSnapshot
	FailingChecks []string
}
