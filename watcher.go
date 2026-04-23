package main

import (
	"context"
	"fmt"
	"log"
	"math/rand"
	"slices"
	"strings"
	"time"
)

type watcher struct {
	client                    gqlClient
	notifier                  notifier
	sleeper                   sleeper
	tracker                   *tracker
	viewerLogin               string
	orgs                      []string
	nowRand                   *rand.Rand
	logger                    *log.Logger
	debug                     bool
	includeApprovedDependabot bool
}

func newWatcher(client gqlClient, notifier notifier, sleeper sleeper, viewer string, orgs []string) *watcher {
	return &watcher{
		client:                    client,
		notifier:                  notifier,
		sleeper:                   sleeper,
		tracker:                   newTracker(),
		viewerLogin:               viewer,
		orgs:                      orgs,
		nowRand:                   rand.New(rand.NewSource(time.Now().UnixNano())),
		logger:                    log.Default(),
		includeApprovedDependabot: true,
	}
}

func (w *watcher) withDebug(enabled bool) *watcher {
	w.debug = enabled
	return w
}

func (w *watcher) withApprovedDependabot(enabled bool) *watcher {
	w.includeApprovedDependabot = enabled
	return w
}

func (w *watcher) debugf(format string, args ...any) {
	if !w.debug || w.logger == nil {
		return
	}
	w.logger.Printf(format, args...)
}

func (w *watcher) logMonitoredPRs(prs []prSnapshot) {
	if !w.debug {
		return
	}
	if len(prs) == 0 {
		w.debugf("Monitored PRs: none")
		return
	}

	sorted := append([]prSnapshot(nil), prs...)
	slices.SortFunc(sorted, func(a, b prSnapshot) int {
		if a.Repo != b.Repo {
			return strings.Compare(a.Repo, b.Repo)
		}
		if a.Number == b.Number {
			return strings.Compare(a.ID, b.ID)
		}
		if a.Number < b.Number {
			return -1
		}
		return 1
	})

	w.debugf("Monitored PRs (%d):", len(sorted))
	for _, pr := range sorted {
		w.debugf(
			"- %s state=%s mergeable=%s merge_state=%s checks(head=%s queue=%s) queue=%t",
			pr.ref(),
			pr.State,
			pr.Mergeable,
			pr.MergeStateStatus,
			pr.HeadRollupState,
			pr.QueueRollupState,
			pr.QueueEntryPresent,
		)
	}
}

func (w *watcher) run(ctx context.Context, once bool) error {
	lastKnownRate := rateLimitInfo{}
	for {
		events, rl, err := w.poll(ctx)
		lastKnownRate = mergeRateLimit(lastKnownRate, rl)
		if err != nil {
			if isRateLimitErr(err) {
				sleepFor := 30 * time.Second
				if lastKnownRate.Valid && lastKnownRate.ResetAt.After(w.sleeper.Now()) {
					sleepFor = lastKnownRate.ResetAt.Sub(w.sleeper.Now()) + w.jitter(2*time.Second)
				}
				if once {
					return nil
				}
				_ = w.sleeper.Sleep(ctx, sleepFor)
				continue
			}
			log.Printf("transient poll error: %v", err)
			if once {
				return nil
			}
			_ = w.sleeper.Sleep(ctx, basePollInterval)
			continue
		}
		lastKnownRate = rl
		for _, evt := range events {
			// Notification failures are non-fatal; polling should continue
			if err := w.notifyEvent(ctx, evt); err != nil {
				log.Printf("notification error: %v", err)
			}
		}
		if once {
			if len(events) == 0 {
				fmt.Println("No PR events detected.")
			} else {
				fmt.Printf("Detected %d PR event(s).\n", len(events))
			}
			return nil
		}
		sleepFor := chooseSleep(basePollInterval, rl, w.sleeper.Now())
		if err := w.sleeper.Sleep(ctx, sleepFor); err != nil {
			return err
		}
	}
}

func (w *watcher) jitter(max time.Duration) time.Duration {
	if max <= 0 {
		return 0
	}
	return time.Duration(w.nowRand.Int63n(int64(max)))
}

func (w *watcher) poll(ctx context.Context) ([]event, rateLimitInfo, error) {
	openPRs := make([]prSnapshot, 0)
	totalRate := rateLimitInfo{}
	for _, org := range w.orgs {
		items, rl, err := w.searchOpenPRsByOrg(ctx, org)
		if err != nil {
			return nil, totalRate, err
		}
		totalRate = mergeRateLimit(totalRate, rl)
		openPRs = append(openPRs, items...)
		w.debugf("Org %s returned %d monitored PR(s)", org, len(items))
		if len(openPRs) >= maxOpenPRs {
			// Stop querying additional orgs once cap is reached to bound API usage
			openPRs = openPRs[:maxOpenPRs]
			break
		}
	}
	w.logMonitoredPRs(openPRs)

	events, failingIDs, disappeared := w.tracker.applyOpenPRs(openPRs)
	w.debugf(
		"Poll summary: events=%d failing=%d disappeared=%d",
		len(events),
		len(failingIDs),
		len(disappeared),
	)
	if len(disappeared) > 0 {
		mergedStates, rl, err := w.fetchPRStates(ctx, disappeared)
		if err != nil {
			return nil, totalRate, err
		}
		totalRate = mergeRateLimit(totalRate, rl)
		events = append(events, w.tracker.resolveDisappeared(disappeared, mergedStates)...)
	}
	if len(failingIDs) > 0 {
		namesByID, rl, err := w.fetchFailingCheckNames(ctx, failingIDs)
		if err != nil {
			return nil, totalRate, err
		}
		totalRate = mergeRateLimit(totalRate, rl)
		for i := range events {
			if events[i].Type != eventChecks {
				continue
			}
			events[i].FailingChecks = namesByID[events[i].PR.ID]
		}
	}
	return events, totalRate, nil
}

func (w *watcher) notifyEvent(ctx context.Context, evt event) error {
	n, ok := buildNotification(evt)
	if !ok {
		return nil
	}
	return w.notifier.Notify(ctx, n)
}

func buildNotification(evt event) (notification, bool) {
	title := ""
	urgent := false
	switch evt.Type {
	case eventMerged:
		title = "PR merged"
	case eventDequeued:
		title = "PR removed from merge queue"
	case eventConflict:
		title = "PR has merge conflicts"
	case eventChecks:
		title = "PR checks failed"
		urgent = true
	default:
		return notification{}, false
	}
	lines := []string{fmt.Sprintf("%s: %s", evt.PR.ref(), evt.PR.Title), evt.PR.URL}
	if evt.Type == eventChecks && len(evt.FailingChecks) > 0 {
		lines = append(lines, strings.Join(evt.FailingChecks, "\n"))
	}
	return notification{
		Title:  title,
		Body:   strings.Join(lines, "\n"),
		Urgent: urgent,
	}, true
}

func isRateLimitErr(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "rate limit") || strings.Contains(msg, "api quota exceeded")
}
