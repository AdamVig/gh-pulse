package main

import "slices"

type trackedPR struct {
	Snapshot                prSnapshot
	LastConflictFingerprint string
}

type tracker struct {
	prs map[string]trackedPR
}

func newTracker() *tracker {
	return &tracker{prs: map[string]trackedPR{}}
}

// applyOpenPRs emits edge-triggered events by comparing current snapshots against prior state per PR ID
func (t *tracker) applyOpenPRs(current []prSnapshot) (events []event, newlyFailing []string, disappeared []string) {
	seen := make(map[string]struct{}, len(current))
	for _, cur := range current {
		seen[cur.ID] = struct{}{}
		prev, ok := t.prs[cur.ID]
		next := prev
		if ok {
			if prev.Snapshot.QueueEntryPresent && !cur.QueueEntryPresent && cur.State == "OPEN" {
				events = append(events, event{Type: eventDequeued, PR: cur})
			}
			if shouldNotifyConflict(prev, cur) {
				events = append(events, event{Type: eventConflict, PR: cur})
				next.LastConflictFingerprint = conflictFingerprint(cur)
			}
			if !isFailing(prev.Snapshot) && isFailing(cur) && shouldNotifyChecks(prev.Snapshot, cur) {
				events = append(events, event{Type: eventChecks, PR: cur})
				newlyFailing = append(newlyFailing, cur.ID)
			}
		}
		next.Snapshot = cur
		t.prs[cur.ID] = next
	}
	for id, prev := range t.prs {
		if _, ok := seen[id]; ok {
			continue
		}
		// Missing open PRs are resolved later via GraphQL to distinguish merged from other disappearance cases
		if prev.Snapshot.State == "OPEN" {
			disappeared = append(disappeared, id)
		}
	}
	slices.Sort(disappeared)
	return events, newlyFailing, disappeared
}

type resolvedPRState struct {
	State  string
	PR     prSnapshot
	Exists bool
}

// resolveDisappeared emits merged events only for IDs confirmed MERGED and removes resolved IDs from tracking
func (t *tracker) resolveDisappeared(ids []string, resolved map[string]resolvedPRState) []event {
	out := make([]event, 0, len(ids))
	for _, id := range ids {
		tracked, ok := t.prs[id]
		if !ok {
			continue
		}
		state, ok := resolved[id]
		if !ok || !state.Exists {
			continue
		}
		if state.State == "MERGED" {
			pr := tracked.Snapshot
			if state.PR.ID != "" {
				pr = state.PR
			}
			out = append(out, event{Type: eventMerged, PR: pr})
		}
		delete(t.prs, id)
	}
	return out
}

func isConflicting(pr prSnapshot) bool {
	return pr.Mergeable == "CONFLICTING" || pr.MergeStateStatus == "DIRTY"
}

func shouldNotifyConflict(prev trackedPR, cur prSnapshot) bool {
	if isConflicting(prev.Snapshot) || !isConflicting(cur) {
		return false
	}
	return prev.LastConflictFingerprint != conflictFingerprint(cur)
}

func conflictFingerprint(pr prSnapshot) string {
	if pr.HeadOID != "" {
		return "head:" + pr.HeadOID
	}
	if pr.QueueHeadOID != "" {
		return "queue:" + pr.QueueHeadOID
	}
	return "mergeable:" + pr.Mergeable + "|state:" + pr.MergeStateStatus
}

func isFailing(pr prSnapshot) bool {
	return isFailingRollup(pr.HeadRollupState) || isFailingRollup(pr.QueueRollupState)
}

// Suppress check-failure notifications while queued and on dequeue transitions.
func shouldNotifyChecks(prev, cur prSnapshot) bool {
	return !prev.QueueEntryPresent && !cur.QueueEntryPresent
}

func isFailingRollup(state string) bool {
	return state == "FAILURE" || state == "ERROR"
}
