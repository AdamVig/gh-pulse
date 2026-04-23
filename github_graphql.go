package main

import (
	"context"
	"fmt"
	"time"
)

const dependabotAuthorLogin = "dependabot[bot]"

func monitoredPRSearchQuery(viewerLogin, org string) string {
	return fmt.Sprintf(
		"is:pr is:open org:%s author:%s",
		org,
		viewerLogin,
	)
}

func approvedDependabotSearchQuery(viewerLogin, org string) string {
	return fmt.Sprintf(
		"is:pr is:open org:%s author:%s review:approved reviewed-by:%s",
		org,
		dependabotAuthorLogin,
		viewerLogin,
	)
}

func (w *watcher) searchOpenPRsByOrg(ctx context.Context, org string) ([]prSnapshot, rateLimitInfo, error) {
	query := `query($query:String!, $after:String, $dependabotQuery:String!, $dependabotAfter:String, $withAuthored:Boolean!, $withDependabot:Boolean!) {
  Search: search(type: ISSUE, query: $query, first: ` + fmt.Sprintf("%d", searchPageSize) + `, after: $after) @include(if: $withAuthored) {
    pageInfo { hasNextPage endCursor }
    nodes {
      ... on PullRequest {
        id
        number
        title
        url
        state
        updatedAt
        mergedAt
        closedAt
        repository { nameWithOwner }
        mergeable
        mergeStateStatus
        commits(last: 1) {
          nodes {
            commit {
              oid
              statusCheckRollup { state }
            }
          }
        }
        mergeQueueEntry {
          id
          state
          position
          enqueuedAt
          headCommit {
            oid
            statusCheckRollup { state }
          }
        }
      }
    }
  }
  DependabotSearch: search(type: ISSUE, query: $dependabotQuery, first: ` + fmt.Sprintf("%d", searchPageSize) + `, after: $dependabotAfter) @include(if: $withDependabot) {
    pageInfo { hasNextPage endCursor }
    nodes {
      ... on PullRequest {
        id
        number
        title
        url
        state
        updatedAt
        mergedAt
        closedAt
        repository { nameWithOwner }
        mergeable
        mergeStateStatus
        commits(last: 1) {
          nodes {
            commit {
              oid
              statusCheckRollup { state }
            }
          }
        }
        mergeQueueEntry {
          id
          state
          position
          enqueuedAt
          headCommit {
            oid
            statusCheckRollup { state }
          }
        }
      }
    }
  }
  rateLimit { cost remaining resetAt }
}`

	authoredSearch := monitoredPRSearchQuery(w.viewerLogin, org)
	dependabotSearch := approvedDependabotSearchQuery(w.viewerLogin, org)
	authored := make([]prSnapshot, 0)
	dependabot := make([]prSnapshot, 0)
	authoredSeen := make(map[string]struct{})
	dependabotSeen := make(map[string]struct{})
	withAuthored := true
	withDependabot := w.includeApprovedDependabot
	var authoredAfter *string
	var dependabotAfter *string
	totalRL := rateLimitInfo{}

	appendNodes := func(dst *[]prSnapshot, seen map[string]struct{}, nodes []struct {
		ID         string
		Number     int
		Title      string
		URL        string
		State      string
		Mergeable  string
		MergeState string `json:"mergeStateStatus"`
		Repository struct{ NameWithOwner string }
		Commits    struct {
			Nodes []struct {
				Commit struct {
					OID               string
					StatusCheckRollup *struct{ State string }
				}
			}
		}
		MergeQueueEntry *struct {
			ID         string
			HeadCommit *struct {
				OID               string
				StatusCheckRollup *struct{ State string }
			}
		}
	}) {
		for _, n := range nodes {
			if _, ok := seen[n.ID]; ok {
				continue
			}
			seen[n.ID] = struct{}{}
			cur := prSnapshot{
				ID:               n.ID,
				Number:           n.Number,
				Title:            n.Title,
				URL:              n.URL,
				State:            n.State,
				Repo:             n.Repository.NameWithOwner,
				Mergeable:        n.Mergeable,
				MergeStateStatus: n.MergeState,
			}
			if len(n.Commits.Nodes) > 0 {
				cur.HeadOID = n.Commits.Nodes[0].Commit.OID
				if n.Commits.Nodes[0].Commit.StatusCheckRollup != nil {
					cur.HeadRollupState = n.Commits.Nodes[0].Commit.StatusCheckRollup.State
				}
			}
			if n.MergeQueueEntry != nil {
				cur.QueueEntryPresent = true
				cur.QueueEntryID = n.MergeQueueEntry.ID
				if n.MergeQueueEntry.HeadCommit != nil {
					cur.QueueHeadOID = n.MergeQueueEntry.HeadCommit.OID
					if n.MergeQueueEntry.HeadCommit.StatusCheckRollup != nil {
						cur.QueueRollupState = n.MergeQueueEntry.HeadCommit.StatusCheckRollup.State
					}
				}
			}
			*dst = append(*dst, cur)
		}
	}

	for withAuthored || withDependabot {
		resp := struct {
			Search *struct {
				PageInfo struct {
					HasNextPage bool
					EndCursor   string
				}
				Nodes []struct {
					ID         string
					Number     int
					Title      string
					URL        string
					State      string
					Mergeable  string
					MergeState string `json:"mergeStateStatus"`
					Repository struct{ NameWithOwner string }
					Commits    struct {
						Nodes []struct {
							Commit struct {
								OID               string
								StatusCheckRollup *struct{ State string }
							}
						}
					}
					MergeQueueEntry *struct {
						ID         string
						HeadCommit *struct {
							OID               string
							StatusCheckRollup *struct{ State string }
						}
					}
				}
			}
			DependabotSearch *struct {
				PageInfo struct {
					HasNextPage bool
					EndCursor   string
				}
				Nodes []struct {
					ID         string
					Number     int
					Title      string
					URL        string
					State      string
					Mergeable  string
					MergeState string `json:"mergeStateStatus"`
					Repository struct{ NameWithOwner string }
					Commits    struct {
						Nodes []struct {
							Commit struct {
								OID               string
								StatusCheckRollup *struct{ State string }
							}
						}
					}
					MergeQueueEntry *struct {
						ID         string
						HeadCommit *struct {
							OID               string
							StatusCheckRollup *struct{ State string }
						}
					}
				}
			}
			RateLimit struct {
				Cost      int
				Remaining int
				ResetAt   time.Time
			}
		}{}

		vars := map[string]any{
			"query":           authoredSearch,
			"after":           authoredAfter,
			"dependabotQuery": dependabotSearch,
			"dependabotAfter": dependabotAfter,
			"withAuthored":    withAuthored,
			"withDependabot":  withDependabot,
		}
		if err := w.client.DoWithContext(ctx, query, vars, &resp); err != nil {
			return nil, totalRL, err
		}
		totalRL = mergeRateLimit(totalRL, rateLimitInfo{
			Cost:      resp.RateLimit.Cost,
			Remaining: resp.RateLimit.Remaining,
			ResetAt:   resp.RateLimit.ResetAt,
			Valid:     true,
		})

		if withAuthored {
			if resp.Search == nil {
				withAuthored = false
			} else {
				appendNodes(&authored, authoredSeen, resp.Search.Nodes)
				if len(authored) >= maxOpenPRs {
					return authored[:maxOpenPRs], totalRL, nil
				}
				withAuthored = resp.Search.PageInfo.HasNextPage
				if withAuthored {
					c := resp.Search.PageInfo.EndCursor
					authoredAfter = &c
				}
			}
		}

		if withDependabot {
			if resp.DependabotSearch == nil {
				withDependabot = false
			} else {
				appendNodes(&dependabot, dependabotSeen, resp.DependabotSearch.Nodes)
				withDependabot = resp.DependabotSearch.PageInfo.HasNextPage
				if withDependabot {
					c := resp.DependabotSearch.PageInfo.EndCursor
					dependabotAfter = &c
				}
			}
		}

		if !withAuthored {
			remaining := maxOpenPRs - len(authored)
			if remaining <= 0 || len(dependabot) >= remaining {
				withDependabot = false
			}
		}
	}

	all := append([]prSnapshot{}, authored...)
	seen := mapFromPRs(all)
	for _, pr := range dependabot {
		if len(all) >= maxOpenPRs {
			break
		}
		if _, ok := seen[pr.ID]; ok {
			continue
		}
		seen[pr.ID] = struct{}{}
		all = append(all, pr)
	}
	return all, totalRL, nil
}

func mapFromPRs(prs []prSnapshot) map[string]struct{} {
	seen := make(map[string]struct{}, len(prs))
	for _, pr := range prs {
		seen[pr.ID] = struct{}{}
	}
	return seen
}

func (w *watcher) fetchPRStates(ctx context.Context, ids []string) (map[string]resolvedPRState, rateLimitInfo, error) {
	query := `query($ids:[ID!]!) {
  nodes(ids: $ids) {
    ... on PullRequest {
      id
      number
      title
      url
      state
      repository { nameWithOwner }
    }
  }
  rateLimit { cost remaining resetAt }
}`
	resp := struct {
		Nodes []struct {
			ID         string
			Number     int
			Title      string
			URL        string
			State      string
			Repository struct{ NameWithOwner string }
		}
		RateLimit struct {
			Cost      int
			Remaining int
			ResetAt   time.Time
		}
	}{}
	if err := w.client.DoWithContext(ctx, query, map[string]any{"ids": ids}, &resp); err != nil {
		return nil, rateLimitInfo{}, err
	}
	result := make(map[string]resolvedPRState, len(resp.Nodes))
	for _, n := range resp.Nodes {
		if n.ID == "" {
			continue
		}
		result[n.ID] = resolvedPRState{
			Exists: true,
			State:  n.State,
			PR: prSnapshot{
				ID:     n.ID,
				Number: n.Number,
				Title:  n.Title,
				URL:    n.URL,
				Repo:   n.Repository.NameWithOwner,
				State:  n.State,
			},
		}
	}
	rl := rateLimitInfo{Cost: resp.RateLimit.Cost, Remaining: resp.RateLimit.Remaining, ResetAt: resp.RateLimit.ResetAt, Valid: true}
	return result, rl, nil
}

func (w *watcher) fetchFailingCheckNames(ctx context.Context, ids []string) (map[string][]string, rateLimitInfo, error) {
	query := `query($ids:[ID!]!) {
  nodes(ids: $ids) {
    ... on PullRequest {
      id
      commits(last: 1) {
        nodes {
          commit {
            statusCheckRollup {
              contexts(first: ` + fmt.Sprintf("%d", maxFailingContext) + `) {
                nodes {
                  __typename
                  ... on CheckRun {
                    name
                    conclusion
                  }
                  ... on StatusContext {
                    context
                    state
                  }
                }
              }
            }
          }
        }
      }
      mergeQueueEntry {
        headCommit {
          statusCheckRollup {
            contexts(first: ` + fmt.Sprintf("%d", maxFailingContext) + `) {
              nodes {
                __typename
                ... on CheckRun {
                  name
                  conclusion
                }
                ... on StatusContext {
                  context
                  state
                }
              }
            }
          }
        }
      }
    }
  }
  rateLimit { cost remaining resetAt }
}`

	resp := struct {
		Nodes []struct {
			ID      string
			Commits struct {
				Nodes []struct {
					Commit struct {
						StatusCheckRollup *struct {
							Contexts struct {
								Nodes []rawContext
							}
						}
					}
				}
			}
			MergeQueueEntry *struct {
				HeadCommit *struct {
					StatusCheckRollup *struct {
						Contexts struct {
							Nodes []rawContext
						}
					}
				}
			}
		}
		RateLimit struct {
			Cost      int
			Remaining int
			ResetAt   time.Time
		}
	}{}
	if err := w.client.DoWithContext(ctx, query, map[string]any{"ids": ids}, &resp); err != nil {
		return nil, rateLimitInfo{}, err
	}

	result := map[string][]string{}
	for _, n := range resp.Nodes {
		if n.ID == "" {
			continue
		}
		payload := checkContextPayload{}
		if len(n.Commits.Nodes) > 0 {
			rollup := n.Commits.Nodes[0].Commit.StatusCheckRollup
			if rollup != nil {
				payload.HeadContexts = append(payload.HeadContexts, normalizeContexts(rollup.Contexts.Nodes)...)
			}
		}
		if n.MergeQueueEntry != nil && n.MergeQueueEntry.HeadCommit != nil {
			rollup := n.MergeQueueEntry.HeadCommit.StatusCheckRollup
			if rollup != nil {
				payload.QueueContexts = append(payload.QueueContexts, normalizeContexts(rollup.Contexts.Nodes)...)
			}
		}
		result[n.ID] = extractFailingCheckNames(payload, maxFailureNames)
	}
	rl := rateLimitInfo{Cost: resp.RateLimit.Cost, Remaining: resp.RateLimit.Remaining, ResetAt: resp.RateLimit.ResetAt, Valid: true}
	return result, rl, nil
}

func lookupViewer(ctx context.Context, client gqlClient) (string, rateLimitInfo, error) {
	query := `query {
  viewer { login }
  rateLimit { cost remaining resetAt }
}`
	resp := struct {
		Viewer struct {
			Login string
		}
		RateLimit struct {
			Cost      int
			Remaining int
			ResetAt   time.Time
		}
	}{}
	if err := client.DoWithContext(ctx, query, nil, &resp); err != nil {
		return "", rateLimitInfo{}, err
	}
	rl := rateLimitInfo{Cost: resp.RateLimit.Cost, Remaining: resp.RateLimit.Remaining, ResetAt: resp.RateLimit.ResetAt, Valid: true}
	return resp.Viewer.Login, rl, nil
}
