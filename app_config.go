package main

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/cli/go-gh/v2/pkg/api"
)

const (
	basePollInterval  = 20 * time.Second
	maxOpenPRs        = 300
	searchPageSize    = 50
	maxFailingContext = 50
	maxFailureNames   = 8
)

var graphQLClientFactory = func() (gqlClient, error) {
	return api.DefaultGraphQLClient()
}

var notifierFactory = func() notifier {
	return newNotifier()
}

var sleeperFactory = func() sleeper {
	return realSleeper{}
}

type gqlClient interface {
	DoWithContext(ctx context.Context, query string, variables map[string]any, response any) error
}

type notifier interface {
	Notify(ctx context.Context, n notification) error
}

type sleeper interface {
	Now() time.Time
	Sleep(ctx context.Context, d time.Duration) error
}

type orgListFlag []string

func (f *orgListFlag) String() string {
	return strings.Join(*f, ",")
}

func (f *orgListFlag) Set(value string) error {
	orgs, err := normalizeOrgs(append(append([]string{}, *f...), value))
	if err != nil {
		return err
	}
	*f = orgs
	return nil
}

func normalizeOrgs(values []string) ([]string, error) {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		org := strings.TrimSpace(value)
		if org == "" {
			continue
		}
		if strings.Contains(org, ",") {
			return nil, errors.New("pass multiple organizations with repeated --org flags, not commas")
		}
		if _, ok := seen[org]; ok {
			continue
		}
		seen[org] = struct{}{}
		out = append(out, org)
	}
	if len(values) > 0 && len(out) == 0 {
		return nil, errors.New("organization list cannot be empty")
	}
	return out, nil
}

func resolveWatchedOrgs(overrides []string) ([]string, error) {
	if len(overrides) > 0 {
		return overrides, nil
	}
	return nil, errors.New("at least one --org is required")
}
