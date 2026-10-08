## What changed

<!-- A sentence or two on the behavior change, not a restatement of the diff. -->

## Why

<!-- The problem this solves. Link an issue if there is one. -->

## Checklist

- [ ] `make check` passes (gofmt, `go vet`, unit tests with `-race`)
- [ ] New or changed behavior has unit test coverage
- [ ] `CHANGELOG.md` updated under `## [Unreleased]` for user-visible changes
- [ ] Integration tests run with `make test-integration` if the change touches provisioning, the
      runtime setup, or shutdown (`-tags=integration` creates real tunnels)
- [ ] No new `replace` directives in `go.mod`, and no new public API without a doc comment
