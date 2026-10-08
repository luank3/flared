# Contributing

Thanks for taking the time to contribute. This document covers the local workflow and the testing
rules that matter most for this project.

## Requirements

- Go 1.26 or newer (`go version`).
- Network access for integration tests only; unit tests run offline.
- A Cloudflare account and a `cert.pem` (`cloudflared tunnel login`) only if you exercise the
  Named Tunnel path, which mutates that account.

## Workflow

```bash
make build      # compile the library and the examples
make check      # gofmt check + go vet + unit tests with -race (what CI runs)
make cover      # unit tests with coverage, then a per-function report
make tidy       # go mod tidy && go mod verify
```

Run `make help` for the full list. The Makefile is a convenience layer: every target is a plain `go`
command you can run directly.

`make check` is the gate to pass before opening a pull request: gofmt, module tidiness, `go vet`
(both tag sets) and unit tests with `-race`. CI (`.github/workflows/ci.yml`) runs those steps plus
`go build ./...` and a coverage summary, on pull requests and on pushes to `main`.

## Testing

`flared` has two layers of tests.

**Unit tests** (`flared_test.go`, `provision_test.go`) run offline and cover option validation,
hostname normalization, protocol selection, quick-tunnel response parsing, logging behavior and the
process-wide runtime guard. Always keep coverage for new logic here.

**Integration tests** (`flared_integration_test.go`, build tag `integration`) talk to the real
service:

```bash
make test-integration
# or a single case:
go test -tags=integration -run TestIntegration_QuickTunnel -v -timeout 20m ./...
```

They provision real Quick Tunnels on `trycloudflare.com` — no Cloudflare account or credentials are
needed, but they do require network access and they fail if `api.trycloudflare.com` is unreachable.
What they assert: that `Start` returns a ready tunnel, that the tunnel proxies HTTP traffic to the
`OriginURL`, that an over-short `Timeout` and a cancelled context both fail promptly, and that a
second `Start` in the same process is rejected instead of panicking.

Two properties of the integration tests are deliberate and worth remembering when adding more:

- **One `Start` per process.** cloudflared's runtime (orchestrator, signal handling, Prometheus
  collectors) is process-wide and cannot be rebuilt. Every test that calls `Start` wraps itself in
  `runSubprocess`, which re-runs the single test in a fresh `go test` process. Do the same for new
  cases instead of calling `Start` twice in one test binary.
- **Generous timeouts.** A fresh Quick Tunnel hostname can take tens of seconds before the local
  resolver answers for it (negative caching), so the proxy test polls instead of asserting
  immediately. Keep new network assertions tolerant the same way.

Never point integration tests at a domain you are not willing to mutate: the Named Tunnel path
creates a tunnel and a DNS route in the account that owns `cert.pem`. Tests that need that path
should be opt-in and documented as such.

## Code style

- `gofmt` is mandatory; `go vet` (both with and without the `integration` tag) must be clean.
- Prefer the smallest change that fixes the root cause, and keep exported API comments in the
  `Options`/`Start` style already used in `flared.go`.
- Unexported helpers stay unexported; the public surface is `Options`, `Tunnel` and `Start`.
- New configuration goes through `Options` rather than package-level state, except where
  cloudflared's process-wide runtime forces the guard in `runtime.go` — explain that kind of state
  with a comment.

## Commits and pull requests

- Commit messages follow the existing style: a short imperative subject with a `fix:`/`refactor:`
  style prefix when it helps, then a body listing the meaningful changes.
- One logical change per pull request; include tests for behavior changes.
- Update `CHANGELOG.md` under `## [Unreleased]` for anything user-visible, and mention if the change
  requires a dependency bump of `cloudflared`.

## Releases

Versions are tagged `vYYYY.M.PATCH` (Calendar Versioning), for example `v2026.5.2`. A release is a
tag on `main` plus a `CHANGELOG.md` entry for that version; the module is consumed directly from the
tag, so there are no build artifacts to publish.
