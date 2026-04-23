package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/gen2brain/beeep"
)

type realSleeper struct{}

func (realSleeper) Now() time.Time { return time.Now() }

func (realSleeper) Sleep(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

const notificationAppName = "gh-pulse"

type notification struct {
	Title  string
	Body   string
	Urgent bool
}

type notificationBackend interface {
	Notify(title, body string, icon any) error
	Alert(title, body string, icon any) error
}

type beeepBackend struct{}

func (beeepBackend) Notify(title, body string, icon any) error {
	return beeep.Notify(title, body, icon)
}

func (beeepBackend) Alert(title, body string, icon any) error {
	return beeep.Alert(title, body, icon)
}

type desktopNotifier struct {
	backend  notificationBackend
	fallback io.Writer
	icon     any
}

func newNotifier() *desktopNotifier {
	beeep.AppName = notificationAppName
	return &desktopNotifier{
		backend:  beeepBackend{},
		fallback: os.Stdout,
		icon:     "",
	}
}

func newDesktopNotifier(backend notificationBackend, fallback io.Writer, icon any) *desktopNotifier {
	return &desktopNotifier{
		backend:  backend,
		fallback: fallback,
		icon:     icon,
	}
}

func (n *desktopNotifier) Notify(_ context.Context, msg notification) error {
	if n.backend == nil {
		n.printFallback(msg)
		return nil
	}

	var err error
	if msg.Urgent {
		err = n.backend.Alert(msg.Title, msg.Body, n.icon)
	} else {
		err = n.backend.Notify(msg.Title, msg.Body, n.icon)
	}
	if err != nil {
		n.printFallback(msg)
	}
	return nil
}

func (n *desktopNotifier) printFallback(msg notification) {
	if n.fallback == nil {
		return
	}
	_, _ = fmt.Fprintf(n.fallback, "%s\n%s\n\n", msg.Title, msg.Body)
}
