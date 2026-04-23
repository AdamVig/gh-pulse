package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestChooseSleep(t *testing.T) {
	now := time.Date(2026, 2, 27, 12, 0, 0, 0, time.UTC)
	base := 20 * time.Second

	require.Equal(t, base, chooseSleep(base, rateLimitInfo{}, now))
	require.Equal(t, base, chooseSleep(base, rateLimitInfo{Valid: true, Remaining: 3000, ResetAt: now.Add(1 * time.Hour)}, now))
	require.Greater(t, chooseSleep(base, rateLimitInfo{Valid: true, Remaining: 10, ResetAt: now.Add(1 * time.Hour)}, now), base)
	require.Equal(t, 32*time.Second, chooseSleep(base, rateLimitInfo{Valid: true, Remaining: 0, ResetAt: now.Add(30 * time.Second)}, now))
}

func TestChooseSleepResetAtPastUsesBase(t *testing.T) {
	now := time.Date(2026, 2, 27, 12, 0, 0, 0, time.UTC)
	base := 20 * time.Second
	rl := rateLimitInfo{Valid: true, Remaining: 1, ResetAt: now.Add(-1 * time.Minute)}
	require.Equal(t, base, chooseSleep(base, rl, now))
}

func TestMergeRateLimit(t *testing.T) {
	now := time.Date(2026, 2, 27, 12, 0, 0, 0, time.UTC)
	left := rateLimitInfo{Valid: true, Cost: 3, Remaining: 100, ResetAt: now.Add(1 * time.Hour)}
	right := rateLimitInfo{Valid: true, Cost: 2, Remaining: 50, ResetAt: now.Add(30 * time.Minute)}
	got := mergeRateLimit(left, right)
	require.Equal(t, 5, got.Cost)
	require.Equal(t, 50, got.Remaining)
	require.Equal(t, now.Add(30*time.Minute), got.ResetAt)
	require.True(t, got.Valid)
}
