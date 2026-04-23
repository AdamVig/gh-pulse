package main

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestExtractFailingCheckNamesHandlesKindsDedupeAndCap(t *testing.T) {
	payload := checkContextPayload{
		HeadContexts: []checkContext{
			{Kind: "CheckRun", Name: "build", Conclusion: "FAILURE"},
			{Kind: "CheckRun", Name: "slow", Conclusion: "TIMED_OUT"},
			{Kind: "StatusContext", Name: "lint", State: "ERROR"},
			{Kind: "CheckRun", Name: "build", Conclusion: "FAILURE"},
			{Kind: "CheckRun", Name: "doc", Conclusion: "CANCELLED"},
			{Kind: "CheckRun", Name: "int", Conclusion: "STARTUP_FAILURE"},
			{Kind: "CheckRun", Name: "action", Conclusion: "ACTION_REQUIRED"},
			{Kind: "StatusContext", Name: "qa", State: "FAILURE"},
			{Kind: "CheckRun", Name: "extra", Conclusion: "FAILURE"},
		},
		QueueContexts: []checkContext{{Kind: "StatusContext", Name: "queue-lint", State: "FAILURE"}},
	}

	names := extractFailingCheckNames(payload, 8)
	require.Equal(t, []string{"build", "slow", "lint", "doc", "int", "action", "qa", "extra", "+1 more"}, names)
}

func TestNormalizeContexts(t *testing.T) {
	raw := []rawContext{{Type: "CheckRun", Name: "build", Conclusion: "FAILURE"}, {Type: "StatusContext", Context: "lint", State: "ERROR"}}
	got := normalizeContexts(raw)
	require.Equal(t, []checkContext{{Kind: "CheckRun", Name: "build", Conclusion: "FAILURE", State: ""}, {Kind: "StatusContext", Name: "lint", Conclusion: "", State: "ERROR"}}, got)
}

func TestIsFailingCheckRunConclusionFalseCases(t *testing.T) {
	for _, conclusion := range []string{"SUCCESS", "NEUTRAL", "SKIPPED", ""} {
		require.False(t, isFailingCheckRunConclusion(conclusion))
	}
}

func TestExtractFailingCheckNamesSkipsEmptyNames(t *testing.T) {
	payload := checkContextPayload{
		HeadContexts: []checkContext{
			{Kind: "CheckRun", Name: "", Conclusion: "FAILURE"},
			{Kind: "StatusContext", Name: "", State: "ERROR"},
			{Kind: "CheckRun", Name: "build", Conclusion: "FAILURE"},
		},
	}

	names := extractFailingCheckNames(payload, 8)
	require.Equal(t, []string{"build"}, names)
}
