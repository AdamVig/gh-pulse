package main

import (
	"context"
	"encoding/json"
	"math/rand"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type scriptedCall struct {
	name     string
	assert   func(t *testing.T, query string, variables map[string]any)
	response string
	err      error
}

const fixedResetAt = "2026-02-27T13:00:00Z"
const mainHelperEnv = "GH_PULSE_TEST_MAIN_HELPER"

var (
	testDefaultWatchedOrgs = []string{"test-org-1", "test-org-2"}
	osStdout               = os.Stdout
	osStderr               = os.Stderr
)

type scriptedClient struct {
	t     *testing.T
	calls []scriptedCall
	next  int
}

func (c *scriptedClient) DoWithContext(_ context.Context, query string, variables map[string]any, response any) error {
	c.t.Helper()
	if c.next >= len(c.calls) {
		require.FailNow(c.t, "unexpected GraphQL call", "query=%q vars=%v", query, variables)
	}
	call := c.calls[c.next]
	c.next++
	if call.assert != nil {
		call.assert(c.t, query, variables)
	}
	if call.response != "" {
		require.NoError(c.t, json.Unmarshal([]byte(call.response), response), "call=%s", call.name)
	}
	return call.err
}

func (c *scriptedClient) assertDone() {
	require.Equal(c.t, len(c.calls), c.next, "not all scripted GraphQL calls were consumed")
}

type notifyCall struct {
	notification notification
}

type recordingNotifier struct {
	calls []notifyCall
	err   error
}

func (n *recordingNotifier) Notify(_ context.Context, msg notification) error {
	n.calls = append(n.calls, notifyCall{notification: msg})
	return n.err
}

type backendCall struct {
	title string
	body  string
	icon  any
}

type recordingBackend struct {
	notifyCalls []backendCall
	alertCalls  []backendCall
	err         error
}

func (b *recordingBackend) Notify(title, body string, icon any) error {
	b.notifyCalls = append(b.notifyCalls, backendCall{title: title, body: body, icon: icon})
	return b.err
}

func (b *recordingBackend) Alert(title, body string, icon any) error {
	b.alertCalls = append(b.alertCalls, backendCall{title: title, body: body, icon: icon})
	return b.err
}

type fakeSleeper struct {
	now    time.Time
	sleeps []time.Duration
	err    error
	errs   []error
}

func (s *fakeSleeper) Now() time.Time {
	return s.now
}

func (s *fakeSleeper) Sleep(_ context.Context, d time.Duration) error {
	s.sleeps = append(s.sleeps, d)
	s.now = s.now.Add(d)
	if len(s.errs) > 0 {
		err := s.errs[0]
		s.errs = s.errs[1:]
		return err
	}
	return s.err
}

func matchQuery(parts ...string) func(*testing.T, string, map[string]any) {
	return func(t *testing.T, query string, _ map[string]any) {
		t.Helper()
		for _, part := range parts {
			require.Contains(t, query, part)
		}
	}
}

func matchSearchVars(login, org, expectedAfter string, expectNilAfter bool) func(*testing.T, string, map[string]any) {
	return matchSearchVarsWithDependabot(login, org, expectedAfter, expectNilAfter, false)
}

func matchSearchVarsWithDependabot(login, org, expectedAfter string, expectNilAfter bool, withDependabot bool) func(*testing.T, string, map[string]any) {
	return func(t *testing.T, query string, variables map[string]any) {
		t.Helper()
		require.Contains(t, query, "Search: search(type: ISSUE")
		require.Contains(t, query, "DependabotSearch: search(type: ISSUE")
		require.Equal(t, monitoredPRSearchQuery(login, org), variables["query"])
		require.Equal(t, approvedDependabotSearchQuery(login, org), variables["dependabotQuery"])
		require.Equal(t, true, variables["withAuthored"])
		require.Equal(t, withDependabot, variables["withDependabot"])
		afterRaw, ok := variables["after"]
		require.True(t, ok)
		if expectNilAfter {
			require.Nil(t, afterRaw)
		} else {
			afterPtr, ok := afterRaw.(*string)
			require.True(t, ok)
			require.NotNil(t, afterPtr)
			require.Equal(t, expectedAfter, *afterPtr)
		}

		dependabotAfterRaw, ok := variables["dependabotAfter"]
		require.True(t, ok)
		require.Nil(t, dependabotAfterRaw)
	}
}

func eventTypes(events []event) []eventType {
	out := make([]eventType, 0, len(events))
	for _, evt := range events {
		out = append(out, evt.Type)
	}
	return out
}

func withRunFactories(t *testing.T, clientFactory func() (gqlClient, error), notifyFactory func() notifier) {
	t.Helper()
	oldClientFactory := graphQLClientFactory
	oldNotifierFactory := notifierFactory
	oldSleeperFactory := sleeperFactory
	oldStdout := stdout
	oldStderr := stderr
	t.Cleanup(func() {
		graphQLClientFactory = oldClientFactory
		notifierFactory = oldNotifierFactory
		sleeperFactory = oldSleeperFactory
		stdout = oldStdout
		stderr = oldStderr
	})
	graphQLClientFactory = clientFactory
	notifierFactory = notifyFactory
	sleeperFactory = func() sleeper { return realSleeper{} }
	stdout = osStdout
	stderr = osStderr
}

func withRunDeps(t *testing.T, clientFactory func() (gqlClient, error), notifyFactory func() notifier, sleepFactory func() sleeper) {
	t.Helper()
	withRunFactories(t, clientFactory, notifyFactory)
	sleeperFactory = sleepFactory
}

func mustJSON(t *testing.T, payload any) string {
	t.Helper()
	raw, err := json.Marshal(payload)
	require.NoError(t, err)
	return string(raw)
}

func rateLimitPayload(remaining int) map[string]any {
	return map[string]any{
		"Cost":      1,
		"Remaining": remaining,
		"ResetAt":   fixedResetAt,
	}
}

type queuePayload struct {
	ID     string
	OID    string
	Rollup string
}

func searchPRNode(id string, number int, title, headOID, headRollup, mergeable, mergeState string, queue *queuePayload) map[string]any {
	node := map[string]any{
		"ID":               id,
		"Number":           number,
		"Title":            title,
		"URL":              "https://example/pr" + strings.TrimPrefix(id, "PR"),
		"State":            "OPEN",
		"mergeable":        mergeable,
		"mergeStateStatus": mergeState,
		"Repository":       map[string]any{"NameWithOwner": "test-org/repo"},
		"Commits": map[string]any{
			"Nodes": []map[string]any{{
				"Commit": map[string]any{
					"OID":               headOID,
					"StatusCheckRollup": map[string]any{"State": headRollup},
				},
			}},
		},
	}
	if queue != nil {
		node["MergeQueueEntry"] = map[string]any{
			"ID": queue.ID,
			"HeadCommit": map[string]any{
				"OID":               queue.OID,
				"StatusCheckRollup": map[string]any{"State": queue.Rollup},
			},
		}
	}
	return node
}

func manySearchNodes(count int) []map[string]any {
	nodes := make([]map[string]any, 0, count)
	for i := 1; i <= count; i++ {
		id := "PR" + strconv.Itoa(i)
		nodes = append(nodes, searchPRNode(id, i, "Title "+strconv.Itoa(i), "head"+strconv.Itoa(i), "SUCCESS", "MERGEABLE", "CLEAN", nil))
	}
	return nodes
}

func searchResponse(t *testing.T, hasNext bool, cursor string, remaining int, nodes ...map[string]any) string {
	t.Helper()
	return mustJSON(t, map[string]any{
		"Search": map[string]any{
			"PageInfo": map[string]any{
				"HasNextPage": hasNext,
				"EndCursor":   cursor,
			},
			"Nodes": nodes,
		},
		"RateLimit": rateLimitPayload(remaining),
	})
}

func combinedSearchResponse(t *testing.T, authoredHasNext bool, authoredCursor string, authoredNodes []map[string]any, dependabotHasNext bool, dependabotCursor string, dependabotNodes []map[string]any, remaining int) string {
	t.Helper()
	return mustJSON(t, map[string]any{
		"Search": map[string]any{
			"PageInfo": map[string]any{
				"HasNextPage": authoredHasNext,
				"EndCursor":   authoredCursor,
			},
			"Nodes": authoredNodes,
		},
		"DependabotSearch": map[string]any{
			"PageInfo": map[string]any{
				"HasNextPage": dependabotHasNext,
				"EndCursor":   dependabotCursor,
			},
			"Nodes": dependabotNodes,
		},
		"RateLimit": rateLimitPayload(remaining),
	})
}

func statesResponse(t *testing.T, remaining int, nodes ...map[string]any) string {
	t.Helper()
	return mustJSON(t, map[string]any{
		"Nodes":     nodes,
		"RateLimit": rateLimitPayload(remaining),
	})
}

func viewerResponse(t *testing.T, login string, remaining int) string {
	t.Helper()
	return mustJSON(t, map[string]any{
		"Viewer": map[string]any{
			"Login": login,
		},
		"RateLimit": rateLimitPayload(remaining),
	})
}

func mergedStateNode(id string, number int, title string) map[string]any {
	return map[string]any{
		"ID":     id,
		"Number": number,
		"Title":  title,
		"URL":    "https://example/pr" + strings.TrimPrefix(id, "PR"),
		"State":  "MERGED",
		"Repository": map[string]any{
			"NameWithOwner": "test-org/repo",
		},
	}
}

func checkRun(name, conclusion string) map[string]any {
	return map[string]any{"__typename": "CheckRun", "name": name, "conclusion": conclusion}
}

func statusContext(name, state string) map[string]any {
	return map[string]any{"__typename": "StatusContext", "context": name, "state": state}
}

func failingContextsResponse(t *testing.T, id string, remaining int, headContexts []map[string]any, queueContexts []map[string]any) string {
	t.Helper()
	node := map[string]any{
		"ID": id,
		"Commits": map[string]any{
			"Nodes": []map[string]any{{
				"Commit": map[string]any{
					"StatusCheckRollup": map[string]any{
						"Contexts": map[string]any{"Nodes": headContexts},
					},
				},
			}},
		},
	}
	if len(queueContexts) > 0 {
		node["MergeQueueEntry"] = map[string]any{
			"HeadCommit": map[string]any{
				"StatusCheckRollup": map[string]any{
					"Contexts": map[string]any{"Nodes": queueContexts},
				},
			},
		}
	}
	return statesResponse(t, remaining, node)
}

func newTestWatcher(t *testing.T, calls []scriptedCall, orgs ...string) (*watcher, *scriptedClient) {
	t.Helper()
	if len(orgs) == 0 {
		orgs = []string{"test-org"}
	}
	client := &scriptedClient{t: t, calls: calls}
	w := newWatcher(client, &recordingNotifier{}, &fakeSleeper{now: time.Now()}, "me", orgs).withApprovedDependabot(false)
	return w, client
}

func withArgs(t *testing.T, args []string) {
	t.Helper()
	prev := os.Args
	os.Args = append([]string(nil), args...)
	t.Cleanup(func() {
		os.Args = prev
	})
}

func mainOnceSuccessCalls(t *testing.T) []scriptedCall {
	t.Helper()
	return []scriptedCall{
		{name: "viewer", assert: matchQuery("viewer { login }", "rateLimit"), response: viewerResponse(t, "me", 5000)},
		{name: "search-org1", assert: matchSearchVarsWithDependabot("me", testDefaultWatchedOrgs[0], "", true, true), response: combinedSearchResponse(t, false, "", nil, false, "", nil, 4999)},
		{name: "search-org2", assert: matchSearchVarsWithDependabot("me", testDefaultWatchedOrgs[1], "", true, true), response: combinedSearchResponse(t, false, "", nil, false, "", nil, 4998)},
	}
}

func randSource(seed int64) *rand.Rand {
	return rand.New(rand.NewSource(seed))
}

func testOrgArgs() []string {
	return []string{"--org", testDefaultWatchedOrgs[0], "--org", testDefaultWatchedOrgs[1]}
}
