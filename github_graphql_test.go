package main

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSearchOpenPRsByOrgPagination(t *testing.T) {
	w, client := newTestWatcher(t, []scriptedCall{
		{
			name:   "page-1",
			assert: matchSearchVars("me", "test-org", "", true),
			response: searchResponse(t, true, "CUR1", 4000,
				searchPRNode("PR1", 1, "One", "head1", "SUCCESS", "MERGEABLE", "CLEAN", &queuePayload{ID: "MQ1", OID: "q1", Rollup: "PENDING"}),
			),
		},
		{
			name:   "page-2",
			assert: matchSearchVars("me", "test-org", "CUR1", false),
			response: searchResponse(t, false, "", 3998,
				searchPRNode("PR2", 2, "Two", "head2", "ERROR", "CONFLICTING", "DIRTY", nil),
			),
		},
	})
	prs, rl, err := w.searchOpenPRsByOrg(context.Background(), "test-org")
	require.NoError(t, err)
	client.assertDone()

	require.Len(t, prs, 2)
	require.True(t, prs[0].QueueEntryPresent)
	require.Equal(t, "MQ1", prs[0].QueueEntryID)
	require.Equal(t, "PENDING", prs[0].QueueRollupState)
	require.Equal(t, "DIRTY", prs[1].MergeStateStatus)
	require.Equal(t, "ERROR", prs[1].HeadRollupState)
	require.True(t, rl.Valid)
	require.Equal(t, 3998, rl.Remaining)
}

func TestSearchOpenPRsByOrgIncludesApprovedDependabotWhenEnabled(t *testing.T) {
	w, client := newTestWatcher(t, []scriptedCall{
		{
			name:   "combined",
			assert: matchSearchVarsWithDependabot("me", "test-org", "", true, true),
			response: combinedSearchResponse(
				t,
				false,
				"",
				[]map[string]any{
					searchPRNode("PR1", 1, "Mine", "head1", "SUCCESS", "MERGEABLE", "CLEAN", nil),
				},
				false,
				"",
				[]map[string]any{
					searchPRNode("PR2", 2, "Bot", "head2", "SUCCESS", "MERGEABLE", "CLEAN", nil),
				},
				3999,
			),
		},
	})
	w.includeApprovedDependabot = true

	prs, rl, err := w.searchOpenPRsByOrg(context.Background(), "test-org")
	require.NoError(t, err)
	client.assertDone()

	require.Len(t, prs, 2)
	require.Equal(t, "PR1", prs[0].ID)
	require.Equal(t, "PR2", prs[1].ID)
	require.True(t, rl.Valid)
	require.Equal(t, 3999, rl.Remaining)
}

func TestSearchOpenPRsByOrgRespectsCap(t *testing.T) {
	nodes := manySearchNodes(maxOpenPRs + 20)
	w, client := newTestWatcher(t, []scriptedCall{
		{
			name:     "page-1",
			assert:   matchSearchVars("me", "test-org", "", true),
			response: searchResponse(t, true, "CUR1", 4000, nodes...),
		},
		{
			name:   "unused-second-page",
			assert: matchSearchVars("me", "test-org", "CUR1", false),
			err:    errors.New("should not be called"),
		},
	})

	prs, _, err := w.searchOpenPRsByOrg(context.Background(), "test-org")
	require.NoError(t, err)
	require.Len(t, prs, maxOpenPRs)
	require.Equal(t, "PR1", prs[0].ID)
	require.Equal(t, "PR300", prs[len(prs)-1].ID)
	require.Equal(t, 1, client.next)
}

func TestSearchOpenPRsByOrgReturnsErrorOnLaterPage(t *testing.T) {
	w, client := newTestWatcher(t, []scriptedCall{
		{
			name:   "page-1",
			assert: matchSearchVars("me", "test-org", "", true),
			response: searchResponse(t, true, "CUR1", 4000,
				searchPRNode("PR1", 1, "One", "head1", "SUCCESS", "MERGEABLE", "CLEAN", nil),
			),
		},
		{
			name:   "page-2-fail",
			assert: matchSearchVars("me", "test-org", "CUR1", false),
			err:    errors.New("page 2 failed"),
		},
	})

	prs, rl, err := w.searchOpenPRsByOrg(context.Background(), "test-org")
	require.ErrorContains(t, err, "page 2 failed")
	require.Nil(t, prs)
	require.True(t, rl.Valid)
	require.Equal(t, 4000, rl.Remaining)
	client.assertDone()
}

func TestFetchPRStatesSkipsNullNodes(t *testing.T) {
	w, client := newTestWatcher(t, []scriptedCall{{
		name:   "states",
		assert: matchQuery("nodes(ids: $ids)", "rateLimit"),
		response: statesResponse(t, 4000,
			map[string]any{},
			mergedStateNode("PR2", 2, "Merged"),
		),
	}})
	states, rl, err := w.fetchPRStates(context.Background(), []string{"PR1", "PR2"})
	require.NoError(t, err)
	client.assertDone()
	require.Len(t, states, 1)
	require.Equal(t, "MERGED", states["PR2"].State)
	require.True(t, rl.Valid)
}

func TestFetchFailingCheckNames(t *testing.T) {
	w, client := newTestWatcher(t, []scriptedCall{{
		name:   "checks",
		assert: matchQuery("contexts(first:", "__typename", "rateLimit"),
		response: failingContextsResponse(
			t,
			"PR1",
			4000,
			[]map[string]any{
				checkRun("build", "FAILURE"),
				checkRun("build", "FAILURE"),
				statusContext("lint", "ERROR"),
			},
			[]map[string]any{
				statusContext("queue", "FAILURE"),
			},
		),
	}})
	namesByID, rl, err := w.fetchFailingCheckNames(context.Background(), []string{"PR1"})
	require.NoError(t, err)
	client.assertDone()
	require.Equal(t, []string{"build", "lint", "queue"}, namesByID["PR1"])
	require.True(t, rl.Valid)
}

func TestFetchFailingCheckNamesHandlesMissingRollups(t *testing.T) {
	w, client := newTestWatcher(t, []scriptedCall{{
		name:   "checks-missing-rollups",
		assert: matchQuery("contexts(first:", "__typename", "rateLimit"),
		response: mustJSON(t, map[string]any{
			"Nodes": []map[string]any{
				{
					"ID":      "PR1",
					"Commits": map[string]any{"Nodes": []map[string]any{}},
				},
				{
					"ID": "PR2",
					"Commits": map[string]any{
						"Nodes": []map[string]any{{"Commit": map[string]any{}}},
					},
					"MergeQueueEntry": map[string]any{
						"HeadCommit": nil,
					},
				},
				{
					"ID": "PR3",
					"Commits": map[string]any{
						"Nodes": []map[string]any{{
							"Commit": map[string]any{
								"StatusCheckRollup": map[string]any{
									"Contexts": map[string]any{"Nodes": []map[string]any{}},
								},
							},
						}},
					},
				},
			},
			"RateLimit": rateLimitPayload(4000),
		}),
	}})

	namesByID, rl, err := w.fetchFailingCheckNames(context.Background(), []string{"PR1", "PR2", "PR3"})
	require.NoError(t, err)
	require.Empty(t, namesByID["PR1"])
	require.Empty(t, namesByID["PR2"])
	require.Empty(t, namesByID["PR3"])
	require.True(t, rl.Valid)
	client.assertDone()
}

func TestFetchFailingCheckNamesSkipsEmptyIDNode(t *testing.T) {
	w, client := newTestWatcher(t, []scriptedCall{{
		name:   "checks-skip-empty-id",
		assert: matchQuery("contexts(first:", "__typename", "rateLimit"),
		response: mustJSON(t, map[string]any{
			"Nodes": []map[string]any{
				{
					"ID": "",
					"Commits": map[string]any{
						"Nodes": []map[string]any{{
							"Commit": map[string]any{
								"StatusCheckRollup": map[string]any{
									"Contexts": map[string]any{
										"Nodes": []map[string]any{checkRun("ignored", "FAILURE")},
									},
								},
							},
						}},
					},
				},
				{
					"ID": "PR2",
					"Commits": map[string]any{
						"Nodes": []map[string]any{{
							"Commit": map[string]any{
								"StatusCheckRollup": map[string]any{
									"Contexts": map[string]any{
										"Nodes": []map[string]any{checkRun("build", "FAILURE")},
									},
								},
							},
						}},
					},
				},
			},
			"RateLimit": rateLimitPayload(4000),
		}),
	}})

	namesByID, rl, err := w.fetchFailingCheckNames(context.Background(), []string{"PR1", "PR2"})
	require.NoError(t, err)
	require.True(t, rl.Valid)
	require.Equal(t, []string{"build"}, namesByID["PR2"])
	require.Len(t, namesByID, 1)
	client.assertDone()
}

func TestLookupViewer(t *testing.T) {
	client := &scriptedClient{t: t, calls: []scriptedCall{{
		name:     "viewer",
		assert:   matchQuery("viewer { login }", "rateLimit"),
		response: viewerResponse(t, "me", 1000),
	}}}

	login, rl, err := lookupViewer(context.Background(), client)
	require.NoError(t, err)
	require.Equal(t, "me", login)
	require.True(t, rl.Valid)
	client.assertDone()
}
