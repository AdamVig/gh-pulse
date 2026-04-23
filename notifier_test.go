package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestRealSleeper(t *testing.T) {
	s := realSleeper{}
	require.WithinDuration(t, time.Now(), s.Now(), 500*time.Millisecond)
	require.NoError(t, s.Sleep(context.Background(), 0))

	start := time.Now()
	require.NoError(t, s.Sleep(context.Background(), 1*time.Millisecond))
	require.GreaterOrEqual(t, time.Since(start), time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	require.ErrorIs(t, s.Sleep(ctx, 10*time.Millisecond), context.Canceled)
}

func TestDesktopNotifierDispatchesByUrgency(t *testing.T) {
	backend := &recordingBackend{}
	n := newDesktopNotifier(backend, &bytes.Buffer{}, "icon.png")

	require.NoError(t, n.Notify(context.Background(), notification{
		Title: "Info",
		Body:  "body",
	}))
	require.NoError(t, n.Notify(context.Background(), notification{
		Title:  "Urgent",
		Body:   "body",
		Urgent: true,
	}))

	require.Len(t, backend.notifyCalls, 1)
	require.Len(t, backend.alertCalls, 1)
	require.Equal(t, "Info", backend.notifyCalls[0].title)
	require.Equal(t, "Urgent", backend.alertCalls[0].title)
	require.Equal(t, "icon.png", backend.notifyCalls[0].icon)
	require.Equal(t, "icon.png", backend.alertCalls[0].icon)
}

func TestDesktopNotifierFallbackOnBackendError(t *testing.T) {
	backend := &recordingBackend{err: errors.New("notify failed")}
	var out bytes.Buffer
	n := newDesktopNotifier(backend, &out, "")

	require.NoError(t, n.Notify(context.Background(), notification{
		Title: "Title",
		Body:  "Body",
	}))
	require.Contains(t, out.String(), "Title")
	require.Contains(t, out.String(), "Body")
}

func TestDesktopNotifierFallbackWhenBackendMissing(t *testing.T) {
	var out bytes.Buffer
	n := newDesktopNotifier(nil, &out, "")

	require.NoError(t, n.Notify(context.Background(), notification{
		Title: "Title",
		Body:  "Body",
	}))
	require.Contains(t, out.String(), "Title")
	require.Contains(t, out.String(), "Body")
}

func TestDesktopNotifierFallbackWithNilWriter(t *testing.T) {
	n := newDesktopNotifier(nil, nil, "")

	require.NoError(t, n.Notify(context.Background(), notification{
		Title: "Title",
		Body:  "Body",
	}))
}

func TestNewNotifierDefaults(t *testing.T) {
	n := newNotifier()

	require.IsType(t, beeepBackend{}, n.backend)
	require.Equal(t, os.Stdout, n.fallback)
	require.Equal(t, "", n.icon)
}

func TestBeeepBackendReturnsErrorOnUnsupportedIconType(t *testing.T) {
	b := beeepBackend{}

	require.Error(t, b.Notify("Title", "Body", 123))
	require.Error(t, b.Alert("Title", "Body", 123))
}
