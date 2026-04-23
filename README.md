# gh-pulse

`gh-pulse` is a GitHub CLI extension that watches:

- your authored pull requests, and
- Dependabot-authored pull requests you have approved

in organizations passed via `--org`.

It sends cross-platform desktop notifications when a watched PR:

- is merged,
- is removed from merge queue while still open,
- becomes merge-conflicting, or
- transitions into failing checks.

Check failures are sent as urgent alerts (where supported by the OS notification backend).

The extension uses GitHub GraphQL through authenticated `gh` context via `go-gh` and polls with rate-limit-aware backoff.

## Install

Requirements:

- GitHub CLI (`gh`) installed and authenticated
- Desktop notifications supported by your local environment

```sh
gh extension install AdamVig/gh-pulse
```

From local source:

```sh
go build -o gh-pulse .
gh extension install .
```

## Run

Pass one or more organizations with `--org`:

```sh
gh pulse --org my-org --org another-org
```

Run one poll cycle and exit:

```sh
gh pulse --org my-org --once
```

Watch continuously with debug logging:

```sh
gh pulse --org my-org --org another-org --debug
```

Print a live debug view of monitored PR snapshots each poll:

```sh
gh pulse --org my-org --debug
```

Use both to inspect exactly what one poll sees:

```sh
gh pulse --org my-org --once --debug
```

## Development Commands

Use `make help` to see the canonical local commands:

```sh
make help
```

Common targets:

- `make test` runs the test suite.
- `make test-race` runs tests with the race detector.
- `make vet` runs `go vet`.
- `make fmt` formats Go code.
- `make fix` runs `go fix`.
- `make check` runs `fix`, `fmt`, `vet`, `test`, and `build`.
- `make build` builds the `gh-pulse` binary.

## Architecture

See [`ARCHITECTURE.md`](ARCHITECTURE.md) for data flow and
"where to edit for X" guidance.
