package main

import (
	"context"
	"errors"
	"flag"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestRun(t *testing.T) {
	t.Run("parse-error", func(t *testing.T) {
		err := run(context.Background(), []string{"--bad-flag"})
		require.Error(t, err)
	})

	t.Run("help", func(t *testing.T) {
		stdoutBuf, stderrBuf := withCapturedOutput(t)

		err := run(context.Background(), []string{"--help"})
		require.ErrorIs(t, err, flag.ErrHelp)
		require.Empty(t, stdoutBuf.String())
		require.Contains(t, stderrBuf.String(), "gh pulse [flags]")
	})

	t.Run("missing-org-config", func(t *testing.T) {
		err := run(context.Background(), []string{"--once"})
		require.ErrorContains(t, err, "at least one --org is required")
	})

	t.Run("client-init-failure", func(t *testing.T) {
		withRunFactories(t, func() (gqlClient, error) {
			return nil, errors.New("init fail")
		}, func() notifier { return &recordingNotifier{} })

		err := run(context.Background(), append([]string{"--once"}, testOrgArgs()...))
		require.Error(t, err)
		require.Contains(t, err.Error(), "create GraphQL client")
	})

	t.Run("viewer-lookup-failure", func(t *testing.T) {
		client := &scriptedClient{t: t, calls: []scriptedCall{{name: "viewer", assert: matchQuery("viewer { login }", "rateLimit"), err: errors.New("viewer fail")}}}
		withRunFactories(t, func() (gqlClient, error) { return client, nil }, func() notifier { return &recordingNotifier{} })

		err := run(context.Background(), append([]string{"--once"}, testOrgArgs()...))
		require.Error(t, err)
		require.Contains(t, err.Error(), "lookup viewer")
		client.assertDone()
	})

	t.Run("once-success", func(t *testing.T) {
		client := &scriptedClient{t: t, calls: mainOnceSuccessCalls(t)}
		withRunFactories(t, func() (gqlClient, error) { return client, nil }, func() notifier { return &recordingNotifier{} })

		require.NoError(t, run(context.Background(), append([]string{"--once"}, testOrgArgs()...)))
		client.assertDone()
	})

	t.Run("once-success-debug", func(t *testing.T) {
		client := &scriptedClient{t: t, calls: mainOnceSuccessCalls(t)}
		withRunFactories(t, func() (gqlClient, error) { return client, nil }, func() notifier { return &recordingNotifier{} })

		require.NoError(t, run(context.Background(), append([]string{"--once", "--debug"}, testOrgArgs()...)))
		client.assertDone()
	})

	t.Run("watcher-error-propagates", func(t *testing.T) {
		client := &scriptedClient{t: t, calls: mainOnceSuccessCalls(t)}
		sleepErr := errors.New("sleep fail")
		withRunDeps(
			t,
			func() (gqlClient, error) { return client, nil },
			func() notifier { return &recordingNotifier{} },
			func() sleeper { return &fakeSleeper{now: time.Now(), err: sleepErr} },
		)

		err := run(context.Background(), testOrgArgs())
		require.ErrorIs(t, err, sleepErr)
		client.assertDone()
	})
}

func TestMainInvokesRunDirectly(t *testing.T) {
	client := &scriptedClient{t: t, calls: mainOnceSuccessCalls(t)}
	withRunFactories(t, func() (gqlClient, error) { return client, nil }, func() notifier { return &recordingNotifier{} })
	withArgs(t, append([]string{"gh-pulse", "--once"}, testOrgArgs()...))
	main()
	client.assertDone()
}

func TestMainHelperProcess(t *testing.T) {
	mode := os.Getenv(mainHelperEnv)
	if mode == "" {
		t.Skip("helper subprocess only")
	}
	switch mode {
	case "success":
		client := &scriptedClient{t: t, calls: mainOnceSuccessCalls(t)}
		withRunFactories(t, func() (gqlClient, error) { return client, nil }, func() notifier { return &recordingNotifier{} })
		withArgs(t, append([]string{"gh-pulse", "--once"}, testOrgArgs()...))
		main()
		client.assertDone()
	case "client-fail":
		withRunFactories(t, func() (gqlClient, error) {
			return nil, errors.New("init fail")
		}, func() notifier { return &recordingNotifier{} })
		withArgs(t, append([]string{"gh-pulse", "--once"}, testOrgArgs()...))
		main()
	case "help":
		withArgs(t, []string{"gh-pulse", "--help"})
		main()
	default:
		t.Fatalf("unexpected helper mode %q", mode)
	}
}

func TestMainSubprocessFailure(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=^TestMainHelperProcess$")
	cmd.Env = append(os.Environ(), mainHelperEnv+"=client-fail")
	out, err := cmd.CombinedOutput()
	var exitErr *exec.ExitError
	require.Error(t, err)
	require.ErrorAs(t, err, &exitErr)
	require.NotEqual(t, 0, exitErr.ExitCode())
	require.Contains(t, string(out), "create GraphQL client")
}

func TestMainSubprocessHelp(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=^TestMainHelperProcess$")
	cmd.Env = append(os.Environ(), mainHelperEnv+"=help")
	out, err := cmd.CombinedOutput()
	require.NoError(t, err)
	require.Contains(t, string(out), "gh pulse [flags]")
}
