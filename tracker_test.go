package main

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTrackerTransitions(t *testing.T) {
	tr := newTracker()
	_, _, _ = tr.applyOpenPRs([]prSnapshot{{ID: "P1", State: "OPEN", QueueEntryPresent: true, Mergeable: "MERGEABLE", HeadRollupState: "SUCCESS"}})

	events, failing, disappeared := tr.applyOpenPRs([]prSnapshot{{ID: "P1", State: "OPEN", QueueEntryPresent: false, Mergeable: "CONFLICTING", HeadRollupState: "FAILURE"}})
	require.Equal(t, []eventType{eventDequeued, eventConflict}, eventTypes(events))
	require.Empty(t, failing)
	require.Empty(t, disappeared)
}

func TestTrackerChecksResumeAfterQueueExit(t *testing.T) {
	tr := newTracker()
	_, _, _ = tr.applyOpenPRs([]prSnapshot{{ID: "P1", State: "OPEN", QueueEntryPresent: true, HeadRollupState: "SUCCESS"}})

	events, failing, _ := tr.applyOpenPRs([]prSnapshot{{ID: "P1", State: "OPEN", QueueEntryPresent: false, HeadRollupState: "FAILURE"}})
	require.Equal(t, []eventType{eventDequeued}, eventTypes(events))
	require.Empty(t, failing)

	events, failing, _ = tr.applyOpenPRs([]prSnapshot{{ID: "P1", State: "OPEN", QueueEntryPresent: false, HeadRollupState: "SUCCESS"}})
	require.Empty(t, events)
	require.Empty(t, failing)

	events, failing, _ = tr.applyOpenPRs([]prSnapshot{{ID: "P1", State: "OPEN", QueueEntryPresent: false, HeadRollupState: "FAILURE"}})
	require.Equal(t, []eventType{eventChecks}, eventTypes(events))
	require.Equal(t, []string{"P1"}, failing)
}

func TestTrackerDedupe(t *testing.T) {
	tr := newTracker()
	_, _, _ = tr.applyOpenPRs([]prSnapshot{{ID: "P1", State: "OPEN", QueueEntryPresent: true, Mergeable: "MERGEABLE", HeadRollupState: "SUCCESS"}})
	_, _, _ = tr.applyOpenPRs([]prSnapshot{{ID: "P1", State: "OPEN", QueueEntryPresent: false, Mergeable: "CONFLICTING", HeadRollupState: "FAILURE"}})
	events, _, _ := tr.applyOpenPRs([]prSnapshot{{ID: "P1", State: "OPEN", QueueEntryPresent: false, Mergeable: "CONFLICTING", HeadRollupState: "FAILURE"}})
	require.Empty(t, events)
}

func TestTrackerSuppressesRepeatedConflictForSameHead(t *testing.T) {
	tr := newTracker()
	_, _, _ = tr.applyOpenPRs([]prSnapshot{{ID: "P1", State: "OPEN", HeadOID: "head1", Mergeable: "MERGEABLE"}})

	events, _, _ := tr.applyOpenPRs([]prSnapshot{{ID: "P1", State: "OPEN", HeadOID: "head1", Mergeable: "CONFLICTING"}})
	require.Equal(t, []eventType{eventConflict}, eventTypes(events))

	events, _, _ = tr.applyOpenPRs([]prSnapshot{{ID: "P1", State: "OPEN", HeadOID: "head1", Mergeable: "MERGEABLE"}})
	require.Empty(t, events)

	events, _, _ = tr.applyOpenPRs([]prSnapshot{{ID: "P1", State: "OPEN", HeadOID: "head1", Mergeable: "CONFLICTING"}})
	require.Empty(t, events)
}

func TestTrackerNotifiesConflictAgainAfterHeadChange(t *testing.T) {
	tr := newTracker()
	_, _, _ = tr.applyOpenPRs([]prSnapshot{{ID: "P1", State: "OPEN", HeadOID: "head1", Mergeable: "MERGEABLE"}})
	_, _, _ = tr.applyOpenPRs([]prSnapshot{{ID: "P1", State: "OPEN", HeadOID: "head1", Mergeable: "CONFLICTING"}})
	_, _, _ = tr.applyOpenPRs([]prSnapshot{{ID: "P1", State: "OPEN", HeadOID: "head2", Mergeable: "MERGEABLE"}})

	events, _, _ := tr.applyOpenPRs([]prSnapshot{{ID: "P1", State: "OPEN", HeadOID: "head2", Mergeable: "CONFLICTING"}})
	require.Equal(t, []eventType{eventConflict}, eventTypes(events))
}

func TestTrackerDisappearThenMerged(t *testing.T) {
	tr := newTracker()
	pr := prSnapshot{ID: "P1", Number: 12, Repo: "org/repo", Title: "Title", URL: "https://example", State: "OPEN"}
	_, _, _ = tr.applyOpenPRs([]prSnapshot{pr})
	_, _, disappeared := tr.applyOpenPRs(nil)
	require.Equal(t, []string{"P1"}, disappeared)

	events := tr.resolveDisappeared(disappeared, map[string]resolvedPRState{"P1": {Exists: true, State: "MERGED", PR: prSnapshot{ID: "P1", Number: 12, Repo: "org/repo", Title: "Title", URL: "https://example", State: "MERGED"}}})
	require.Len(t, events, 1)
	require.Equal(t, eventMerged, events[0].Type)
}

func TestResolveDisappearedUsesTrackedSnapshotAndSkipsUnknownID(t *testing.T) {
	tr := newTracker()
	original := prSnapshot{ID: "P1", Number: 12, Repo: "org/repo", Title: "Title", URL: "https://example"}
	_, _, _ = tr.applyOpenPRs([]prSnapshot{original})

	events := tr.resolveDisappeared([]string{"missing", "P1"}, map[string]resolvedPRState{
		"P1": {Exists: true, State: "MERGED", PR: prSnapshot{}},
	})
	require.Len(t, events, 1)
	require.Equal(t, eventMerged, events[0].Type)
	require.Equal(t, original, events[0].PR)
}

func TestResolveDisappearedNonMergedOrMissing(t *testing.T) {
	tr := newTracker()
	_, _, _ = tr.applyOpenPRs([]prSnapshot{
		{ID: "P1", State: "OPEN"},
		{ID: "P2", State: "OPEN"},
	})
	_, _, disappeared := tr.applyOpenPRs(nil)
	require.Equal(t, []string{"P1", "P2"}, disappeared)

	events := tr.resolveDisappeared(disappeared, map[string]resolvedPRState{
		"P1": {Exists: true, State: "CLOSED"},
	})
	require.Empty(t, events)
	_, hasP1 := tr.prs["P1"]
	_, hasP2 := tr.prs["P2"]
	require.False(t, hasP1)
	require.True(t, hasP2)
}
