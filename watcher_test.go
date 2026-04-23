package main

import (
	"bytes"
	"context"
	"errors"
	"log"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestIsRateLimitErr(t *testing.T) {
	require.True(t, isRateLimitErr(errors.New("API rate limit exceeded")))
	require.True(t, isRateLimitErr(errors.New("secondary RATE LIMIT")))
	require.False(t, isRateLimitErr(errors.New("boom")))
	require.False(t, isRateLimitErr(nil))
}

func TestPollSuppressesChecksWhileInMergeQueue(t *testing.T) {
	w, client := newTestWatcher(t, []scriptedCall{
		{
			name:   "seed",
			assert: matchSearchVars("me", "test-org", "", true),
			response: searchResponse(t, false, "", 4000,
				searchPRNode("PR1", 1, "Queued", "head1", "SUCCESS", "MERGEABLE", "CLEAN", &queuePayload{ID: "MQ1", OID: "q1", Rollup: "SUCCESS"}),
			),
		},
		{
			name:   "dequeued-failing",
			assert: matchSearchVars("me", "test-org", "", true),
			response: searchResponse(t, false, "", 3999,
				searchPRNode("PR1", 1, "Queued", "head1b", "FAILURE", "MERGEABLE", "CLEAN", nil),
			),
		},
	})

	events, _, err := w.poll(context.Background())
	require.NoError(t, err)
	require.Empty(t, events)

	events, _, err = w.poll(context.Background())
	require.NoError(t, err)
	client.assertDone()
	require.Equal(t, []eventType{eventDequeued}, eventTypes(events))
}

func TestPollResumesChecksAfterMergeQueueExit(t *testing.T) {
	w, client := newTestWatcher(t, []scriptedCall{
		{
			name:   "seed",
			assert: matchSearchVars("me", "test-org", "", true),
			response: searchResponse(t, false, "", 4000,
				searchPRNode("PR1", 1, "Queued", "head1", "SUCCESS", "MERGEABLE", "CLEAN", &queuePayload{ID: "MQ1", OID: "q1", Rollup: "SUCCESS"}),
			),
		},
		{
			name:   "dequeued-failing",
			assert: matchSearchVars("me", "test-org", "", true),
			response: searchResponse(t, false, "", 3999,
				searchPRNode("PR1", 1, "Queued", "head1b", "FAILURE", "MERGEABLE", "CLEAN", nil),
			),
		},
		{
			name:   "recovered",
			assert: matchSearchVars("me", "test-org", "", true),
			response: searchResponse(t, false, "", 3998,
				searchPRNode("PR1", 1, "Queued", "head1c", "SUCCESS", "MERGEABLE", "CLEAN", nil),
			),
		},
		{
			name:   "refailing",
			assert: matchSearchVars("me", "test-org", "", true),
			response: searchResponse(t, false, "", 3997,
				searchPRNode("PR1", 1, "Queued", "head1d", "FAILURE", "MERGEABLE", "CLEAN", nil),
			),
		},
		{
			name:   "checks",
			assert: matchQuery("contexts(first:"),
			response: failingContextsResponse(t, "PR1", 3996,
				[]map[string]any{checkRun("build", "FAILURE")}, nil,
			),
		},
	})

	events, _, err := w.poll(context.Background())
	require.NoError(t, err)
	require.Empty(t, events)

	events, _, err = w.poll(context.Background())
	require.NoError(t, err)
	require.Equal(t, []eventType{eventDequeued}, eventTypes(events))

	events, _, err = w.poll(context.Background())
	require.NoError(t, err)
	require.Empty(t, events)

	events, _, err = w.poll(context.Background())
	require.NoError(t, err)
	require.Equal(t, []eventType{eventChecks}, eventTypes(events))
	require.Equal(t, []string{"build"}, events[0].FailingChecks)
	client.assertDone()
}

func TestPollDebugLogsMonitoredPRs(t *testing.T) {
	w, client := newTestWatcher(t, []scriptedCall{
		{
			name:   "open-pr",
			assert: matchSearchVars("me", "test-org", "", true),
			response: searchResponse(t, false, "", 4000,
				searchPRNode("PR1", 1, "Queued", "head1", "SUCCESS", "MERGEABLE", "CLEAN", &queuePayload{ID: "MQ1", OID: "q1", Rollup: "SUCCESS"}),
			),
		},
	})

	var out bytes.Buffer
	w.logger = log.New(&out, "", 0)
	w.withDebug(true)

	events, _, err := w.poll(context.Background())
	require.NoError(t, err)
	require.Empty(t, events)
	client.assertDone()

	logs := out.String()
	require.Contains(t, logs, "Org test-org returned 1 monitored PR(s)")
	require.Contains(t, logs, "Monitored PRs (1):")
	require.Contains(t, logs, "test-org/repo#1 state=OPEN")
	require.Contains(t, logs, "Poll summary: events=0 failing=0 disappeared=0")
	require.False(t, strings.Contains(logs, "Monitored PRs: none"))
}

func TestPollEmitsMergedWhenOpenPRDisappears(t *testing.T) {
	w, client := newTestWatcher(t, []scriptedCall{
		{
			name:   "seed",
			assert: matchSearchVars("me", "test-org", "", true),
			response: searchResponse(t, false, "", 4000,
				searchPRNode("PR1", 1, "Title", "head1", "SUCCESS", "MERGEABLE", "CLEAN", nil),
			),
		},
		{
			name:     "disappear",
			assert:   matchSearchVars("me", "test-org", "", true),
			response: searchResponse(t, false, "", 3999),
		},
		{
			name:     "states",
			assert:   matchQuery("nodes(ids: $ids)", "rateLimit"),
			response: statesResponse(t, 3998, mergedStateNode("PR1", 1, "Title")),
		},
	})

	events, _, err := w.poll(context.Background())
	require.NoError(t, err)
	require.Empty(t, events)

	events, _, err = w.poll(context.Background())
	require.NoError(t, err)
	client.assertDone()
	require.Len(t, events, 1)
	require.Equal(t, eventMerged, events[0].Type)
	require.Equal(t, "PR1", events[0].PR.ID)
}

func TestPollDedupeOnUnchangedFailureState(t *testing.T) {
	w, client := newTestWatcher(t, []scriptedCall{
		{
			name:   "seed",
			assert: matchSearchVars("me", "test-org", "", true),
			response: searchResponse(t, false, "", 4000,
				searchPRNode("PR1", 1, "Queued", "head1", "SUCCESS", "MERGEABLE", "CLEAN", nil),
			),
		},
		{
			name:   "first-fail",
			assert: matchSearchVars("me", "test-org", "", true),
			response: searchResponse(t, false, "", 3999,
				searchPRNode("PR1", 1, "Queued", "head1b", "FAILURE", "MERGEABLE", "CLEAN", nil),
			),
		},
		{
			name:   "checks",
			assert: matchQuery("contexts(first:"),
			response: failingContextsResponse(t, "PR1", 3998,
				[]map[string]any{checkRun("build", "FAILURE")}, nil,
			),
		},
		{
			name:   "still-fail",
			assert: matchSearchVars("me", "test-org", "", true),
			response: searchResponse(t, false, "", 3997,
				searchPRNode("PR1", 1, "Queued", "head1c", "FAILURE", "MERGEABLE", "CLEAN", nil),
			),
		},
	})

	events, _, err := w.poll(context.Background())
	require.NoError(t, err)
	require.Empty(t, events)

	events, _, err = w.poll(context.Background())
	require.NoError(t, err)
	require.Equal(t, []eventType{eventChecks}, eventTypes(events))

	events, _, err = w.poll(context.Background())
	require.NoError(t, err)
	require.Empty(t, events)
	client.assertDone()
}

func TestPollReturnsPartialRateLimitOnError(t *testing.T) {
	w, client := newTestWatcher(t, []scriptedCall{
		{
			name:     "org1",
			assert:   matchSearchVars("me", "org1", "", true),
			response: searchResponse(t, false, "", 3500),
		},
		{
			name:   "org2-fail",
			assert: matchSearchVars("me", "org2", "", true),
			err:    errors.New("org2 boom"),
		},
	}, "org1", "org2")

	events, rl, err := w.poll(context.Background())
	require.ErrorContains(t, err, "org2 boom")
	require.Nil(t, events)
	require.True(t, rl.Valid)
	require.Equal(t, 3500, rl.Remaining)
	client.assertDone()
}

func TestPollReturnsErrorWhenStateLookupFails(t *testing.T) {
	w, client := newTestWatcher(t, []scriptedCall{
		{
			name:   "seed",
			assert: matchSearchVars("me", "test-org", "", true),
			response: searchResponse(t, false, "", 4000,
				searchPRNode("PR1", 1, "Title", "head1", "SUCCESS", "MERGEABLE", "CLEAN", nil),
			),
		},
		{
			name:     "disappear",
			assert:   matchSearchVars("me", "test-org", "", true),
			response: searchResponse(t, false, "", 3999),
		},
		{
			name:   "states-fail",
			assert: matchQuery("nodes(ids: $ids)"),
			err:    errors.New("state lookup failed"),
		},
	})

	events, _, err := w.poll(context.Background())
	require.NoError(t, err)
	require.Empty(t, events)

	events, _, err = w.poll(context.Background())
	require.ErrorContains(t, err, "state lookup failed")
	require.Nil(t, events)
	client.assertDone()
}

func TestPollReturnsErrorWhenCheckLookupFails(t *testing.T) {
	w, client := newTestWatcher(t, []scriptedCall{
		{
			name:   "seed",
			assert: matchSearchVars("me", "test-org", "", true),
			response: searchResponse(t, false, "", 4000,
				searchPRNode("PR1", 1, "Queued", "head1", "SUCCESS", "MERGEABLE", "CLEAN", nil),
			),
		},
		{
			name:   "becomes-failing",
			assert: matchSearchVars("me", "test-org", "", true),
			response: searchResponse(t, false, "", 3999,
				searchPRNode("PR1", 1, "Queued", "head1b", "FAILURE", "MERGEABLE", "CLEAN", nil),
			),
		},
		{
			name:   "checks-fail",
			assert: matchQuery("contexts(first:"),
			err:    errors.New("check lookup failed"),
		},
	})

	events, _, err := w.poll(context.Background())
	require.NoError(t, err)
	require.Empty(t, events)

	events, _, err = w.poll(context.Background())
	require.ErrorContains(t, err, "check lookup failed")
	require.Nil(t, events)
	client.assertDone()
}

func TestNotifyEventFormatting(t *testing.T) {
	n := &recordingNotifier{}
	w := newWatcher(&scriptedClient{t: t}, n, &fakeSleeper{now: time.Now()}, "me", []string{"test-org"})

	pr := prSnapshot{ID: "PR1", Number: 1, Repo: "org/repo", Title: "Title", URL: "https://example/pr1"}
	require.NoError(t, w.notifyEvent(context.Background(), event{Type: eventMerged, PR: pr}))
	require.NoError(t, w.notifyEvent(context.Background(), event{Type: eventChecks, PR: pr, FailingChecks: []string{"build", "lint"}}))

	require.Len(t, n.calls, 2)
	require.Equal(t, "PR merged", n.calls[0].notification.Title)
	require.Contains(t, n.calls[0].notification.Body, "org/repo#1: Title")
	require.False(t, n.calls[0].notification.Urgent)
	require.Equal(t, "PR checks failed", n.calls[1].notification.Title)
	require.Contains(t, n.calls[1].notification.Body, "build\nlint")
	require.True(t, n.calls[1].notification.Urgent)
}

func TestNotifyEventEdgeCases(t *testing.T) {
	n := &recordingNotifier{}
	w := newWatcher(&scriptedClient{t: t}, n, &fakeSleeper{now: time.Now()}, "me", []string{"test-org"})
	pr := prSnapshot{ID: "PR1", Number: 1, Repo: "org/repo", Title: "Title", URL: "https://example/pr1"}

	require.NoError(t, w.notifyEvent(context.Background(), event{Type: "unknown", PR: pr}))
	require.Empty(t, n.calls)

	require.NoError(t, w.notifyEvent(context.Background(), event{Type: eventChecks, PR: pr}))
	require.Len(t, n.calls, 1)
	require.Equal(t, "org/repo#1: Title\nhttps://example/pr1", n.calls[0].notification.Body)
}

func TestBuildNotificationContract(t *testing.T) {
	pr := prSnapshot{ID: "PR1", Number: 1, Repo: "org/repo", Title: "Title", URL: "https://example/pr1"}

	tests := []struct {
		name         string
		evt          event
		expectOK     bool
		expectTitle  string
		expectUrgent bool
		expectBody   string
	}{
		{
			name:         "merged",
			evt:          event{Type: eventMerged, PR: pr},
			expectOK:     true,
			expectTitle:  "PR merged",
			expectUrgent: false,
			expectBody:   "org/repo#1: Title\nhttps://example/pr1",
		},
		{
			name:         "dequeued",
			evt:          event{Type: eventDequeued, PR: pr},
			expectOK:     true,
			expectTitle:  "PR removed from merge queue",
			expectUrgent: false,
			expectBody:   "org/repo#1: Title\nhttps://example/pr1",
		},
		{
			name:         "conflict",
			evt:          event{Type: eventConflict, PR: pr},
			expectOK:     true,
			expectTitle:  "PR has merge conflicts",
			expectUrgent: false,
			expectBody:   "org/repo#1: Title\nhttps://example/pr1",
		},
		{
			name:         "checks",
			evt:          event{Type: eventChecks, PR: pr, FailingChecks: []string{"build", "lint"}},
			expectOK:     true,
			expectTitle:  "PR checks failed",
			expectUrgent: true,
			expectBody:   "org/repo#1: Title\nhttps://example/pr1\nbuild\nlint",
		},
		{
			name:     "unknown",
			evt:      event{Type: "unknown", PR: pr},
			expectOK: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := buildNotification(tt.evt)
			require.Equal(t, tt.expectOK, ok)
			if !tt.expectOK {
				return
			}
			require.Equal(t, tt.expectTitle, got.Title)
			require.Equal(t, tt.expectUrgent, got.Urgent)
			require.Equal(t, tt.expectBody, got.Body)
		})
	}
}

func TestPollRespectsCapAcrossOrgs(t *testing.T) {
	nodes := manySearchNodes(maxOpenPRs + 20)
	w, client := newTestWatcher(t, []scriptedCall{
		{
			name:     "org1",
			assert:   matchSearchVars("me", "org1", "", true),
			response: searchResponse(t, false, "", 4000, nodes...),
		},
		{
			name:   "org2-should-not-run",
			assert: matchSearchVars("me", "org2", "", true),
			err:    errors.New("should not query second org"),
		},
	}, "org1", "org2")

	events, rl, err := w.poll(context.Background())
	require.NoError(t, err)
	require.Empty(t, events)
	require.True(t, rl.Valid)
	require.Equal(t, 4000, rl.Remaining)
	require.Equal(t, 1, client.next)
}

func TestWatcherRunContinuousSleepsAdaptively(t *testing.T) {
	start := time.Date(2026, 2, 27, 12, 0, 0, 0, time.UTC)
	s := &fakeSleeper{now: start, err: context.Canceled}
	client := &scriptedClient{t: t, calls: []scriptedCall{{
		name:     "search",
		assert:   matchSearchVars("me", "test-org", "", true),
		response: searchResponse(t, false, "", 10),
	}}}

	w := newWatcher(client, &recordingNotifier{}, s, "me", []string{"test-org"}).withApprovedDependabot(false)
	err := w.run(context.Background(), false)
	require.ErrorIs(t, err, context.Canceled)
	require.Len(t, s.sleeps, 1)
	require.Greater(t, s.sleeps[0], basePollInterval)
	client.assertDone()
}

func TestWatcherRunRateLimitRetryUsesResetBudget(t *testing.T) {
	start := time.Date(2026, 2, 27, 12, 0, 0, 0, time.UTC)
	s := &fakeSleeper{now: start, errs: []error{nil, context.Canceled}}
	client := &scriptedClient{t: t, calls: []scriptedCall{
		{name: "org1-ok", assert: matchSearchVars("me", "org1", "", true), response: searchResponse(t, false, "", 50)},
		{name: "org2-rate-limit", assert: matchSearchVars("me", "org2", "", true), err: errors.New("rate limit exceeded")},
		{name: "org1-recover", assert: matchSearchVars("me", "org1", "", true), response: searchResponse(t, false, "", 5000)},
		{name: "org2-recover", assert: matchSearchVars("me", "org2", "", true), response: searchResponse(t, false, "", 5000)},
	}}

	w := newWatcher(client, &recordingNotifier{}, s, "me", []string{"org1", "org2"}).withApprovedDependabot(false)
	w.nowRand = randSource(1)
	err := w.run(context.Background(), false)
	require.ErrorIs(t, err, context.Canceled)
	require.Len(t, s.sleeps, 2)
	require.GreaterOrEqual(t, s.sleeps[0], time.Hour)
	require.Less(t, s.sleeps[0], time.Hour+2*time.Second)
	require.Equal(t, basePollInterval, s.sleeps[1])
	client.assertDone()
}

func TestWatcherRunTransientErrorSleepsBaseThenRecovers(t *testing.T) {
	s := &fakeSleeper{now: time.Date(2026, 2, 27, 12, 0, 0, 0, time.UTC), errs: []error{nil, context.Canceled}}
	client := &scriptedClient{t: t, calls: []scriptedCall{
		{name: "transient", assert: matchSearchVars("me", "test-org", "", true), err: errors.New("network down")},
		{name: "recovered", assert: matchSearchVars("me", "test-org", "", true), response: searchResponse(t, false, "", 5000)},
	}}

	w := newWatcher(client, &recordingNotifier{}, s, "me", []string{"test-org"}).withApprovedDependabot(false)
	err := w.run(context.Background(), false)
	require.ErrorIs(t, err, context.Canceled)
	require.Equal(t, []time.Duration{basePollInterval, basePollInterval}, s.sleeps)
	client.assertDone()
}

func TestWatcherRunHandlesErrorsWhenOnce(t *testing.T) {
	tests := []struct {
		name string
		err  error
	}{
		{name: "rate-limit", err: errors.New("rate limit exceeded")},
		{name: "transient", err: errors.New("network failure")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &scriptedClient{t: t, calls: []scriptedCall{{name: "search", assert: matchSearchVars("me", "test-org", "", true), err: tt.err}}}
			w := newWatcher(client, &recordingNotifier{}, &fakeSleeper{now: time.Now()}, "me", []string{"test-org"}).withApprovedDependabot(false)
			require.NoError(t, w.run(context.Background(), true))
			client.assertDone()
		})
	}
}

func TestWatcherRunNotifierErrorDoesNotFailLoop(t *testing.T) {
	s := &fakeSleeper{now: time.Date(2026, 2, 27, 12, 0, 0, 0, time.UTC), errs: []error{context.Canceled}}
	n := &recordingNotifier{err: errors.New("notify fail")}
	client := &scriptedClient{t: t, calls: []scriptedCall{
		{
			name:   "search",
			assert: matchSearchVars("me", "test-org", "", true),
			response: searchResponse(t, false, "", 4000,
				searchPRNode("PR1", 1, "Queued", "head1b", "SUCCESS", "MERGEABLE", "CLEAN", nil),
			),
		},
	}}

	w := newWatcher(client, n, s, "me", []string{"test-org"}).withApprovedDependabot(false)
	w.tracker.prs["PR1"] = trackedPR{Snapshot: prSnapshot{
		ID:                "PR1",
		Number:            1,
		Title:             "Queued",
		URL:               "https://example/pr1",
		Repo:              "test-org/repo",
		State:             "OPEN",
		QueueEntryPresent: true,
		HeadRollupState:   "SUCCESS",
	}}

	err := w.run(context.Background(), false)
	require.ErrorIs(t, err, context.Canceled)
	require.Len(t, n.calls, 1)
	require.Equal(t, "PR removed from merge queue", n.calls[0].notification.Title)
	client.assertDone()
}

func TestJitter(t *testing.T) {
	w := &watcher{nowRand: randSource(1)}
	require.Equal(t, time.Duration(0), w.jitter(0))
	got := w.jitter(2 * time.Second)
	require.GreaterOrEqual(t, got, time.Duration(0))
	require.Less(t, got, 2*time.Second)
}
