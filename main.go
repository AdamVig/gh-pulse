package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"strings"
)

var stdout io.Writer = os.Stdout
var stderr io.Writer = os.Stderr

func run(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("gh-pulse", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		fmt.Fprintln(fs.Output(), "Usage:")
		fmt.Fprintln(fs.Output(), "  gh pulse [flags]")
		fmt.Fprintln(fs.Output())
		fmt.Fprintln(fs.Output(), "Flags:")
		fs.PrintDefaults()
	}
	once := fs.Bool("once", false, "Run one poll cycle and exit")
	debug := fs.Bool("debug", false, "Log monitored PR state each poll")
	var orgsFlag orgListFlag
	fs.Var(&orgsFlag, "org", "Watch pull requests in the given organization; repeat for multiple organizations")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		fs.Usage()
		return fmt.Errorf("unexpected arguments: %s", strings.Join(fs.Args(), " "))
	}

	orgs, err := resolveWatchedOrgs(orgsFlag)
	if err != nil {
		return err
	}

	client, err := graphQLClientFactory()
	if err != nil {
		return fmt.Errorf("create GraphQL client: %w", err)
	}
	viewer, _, err := lookupViewer(ctx, client)
	if err != nil {
		return fmt.Errorf("lookup viewer: %w", err)
	}

	w := newWatcher(client, notifierFactory(), sleeperFactory(), viewer, orgs).withDebug(*debug)
	if !*once {
		fmt.Fprintf(
			stdout,
			"Watching authored PRs and approved Dependabot PRs for %s in %s (poll: %s).\n",
			viewer,
			strings.Join(orgs, ", "),
			basePollInterval,
		)
	}
	if err := w.run(ctx, *once); err != nil && !errors.Is(err, context.Canceled) {
		return err
	}
	return nil
}

func main() {
	if err := run(context.Background(), os.Args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			os.Exit(0)
		}
		log.Fatal(err)
	}
}
