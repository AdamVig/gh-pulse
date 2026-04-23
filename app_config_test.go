package main

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNormalizeOrgs(t *testing.T) {
	orgs, err := normalizeOrgs([]string{"org-1", "org-2", "org-2", "org-3"})
	require.NoError(t, err)
	require.Equal(t, []string{"org-1", "org-2", "org-3"}, orgs)

	_, err = normalizeOrgs([]string{","})
	require.ErrorContains(t, err, "pass multiple organizations with repeated --org flags")

	_, err = normalizeOrgs([]string{"   "})
	require.ErrorContains(t, err, "organization list cannot be empty")
}

func TestResolveWatchedOrgs(t *testing.T) {
	t.Run("uses-flag-overrides", func(t *testing.T) {
		orgs, err := resolveWatchedOrgs([]string{"org-a", "org-b"})
		require.NoError(t, err)
		require.Equal(t, []string{"org-a", "org-b"}, orgs)
	})

	t.Run("errors-without-orgs", func(t *testing.T) {
		_, err := resolveWatchedOrgs(nil)
		require.ErrorContains(t, err, "at least one --org is required")
	})
}

func TestOrgListFlagSet(t *testing.T) {
	var orgs orgListFlag
	require.NoError(t, orgs.Set("org-a"))
	require.NoError(t, orgs.Set("org-b"))
	require.Equal(t, []string{"org-a", "org-b"}, []string(orgs))

	err := orgs.Set("org-c,org-d")
	require.ErrorContains(t, err, "repeated --org flags")
}

func withCapturedOutput(t *testing.T) (*bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	oldStdout := stdout
	oldStderr := stderr
	stdoutBuf := &bytes.Buffer{}
	stderrBuf := &bytes.Buffer{}
	stdout = stdoutBuf
	stderr = stderrBuf
	t.Cleanup(func() {
		stdout = oldStdout
		stderr = oldStderr
	})
	return stdoutBuf, stderrBuf
}
