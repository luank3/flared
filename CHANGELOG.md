# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project
adheres to [Calendar Versioning](https://calver.org/) with `vYYYY.M.PATCH` tags.

## [Unreleased]

### Changed

- Module path is now `github.com/luank3/flared` (previously `github.com/lucanhost/flared`, the
  account's former name). Update your imports and `go get` target; GitHub redirects the old path, so
  existing builds keep resolving.

- `Start` now drives cloudflared's runtime packages directly (supervisor, orchestrator, connection,
  ingress, `cfapi`) instead of embedding cloudflared's CLI. No CLI argument parsing, no
  `urfave/cli` app construction, and no process-wide `os.Stderr` redirection.
- Quick Tunnel hostnames are read from the provisioning API response instead of being parsed out of
  cloudflared's log output.
- Internal logs go through a single zerolog logger: `ShowLog` prints to `os.Stderr`, `LogWriter`
  redirects them, and nothing is logged while `ShowLog` is false.
- `Start` returns only once a connection to the Cloudflare edge is established. Startup failures,
  the startup timeout and context cancellation are reported as errors instead of being swallowed.
- `Options.Timeout` bounds the whole `Start` call (provisioning plus connecting) and no longer
  limits the lifetime of the returned `Tunnel`.
- Dependencies updated to `cloudflared v0.0.0-20261005153913-18cdfe0a6fc7` with upstream
  `quic-go v0.59.1` and `urfave/cli v2.3.0`; all `replace` directives were removed, so downstream
  modules no longer need any.

### Added

- `Options.Protocol` to choose the edge transport: `quic`, `http2` or `auto` (QUIC with HTTP/2
  fallback). Quick Tunnels default to `quic` and Named Tunnels to `auto`, as cloudflared does.
- `Options.OverwriteDNS` to control whether routing a Named Tunnel may repoint an existing DNS
  record. Defaults to `false`.
- Named Tunnels reuse an existing tunnel with the same name, and a routing failure is now fatal
  instead of being logged and ignored.
- `Options.LogWriter`.

### Fixed

- A second `Start` in the same process — before or after `Close` — returns an error instead of
  panicking inside `prometheus.MustRegister`, since cloudflared's runtime state is process-wide and
  never unregistered.
- `OriginURL` and `Protocol` are validated before provisioning, so invalid options can no longer
  create a tunnel or a DNS record before failing.
- The readiness timeout reports the configured `Timeout` rather than the remaining budget.
- Integration coverage for proxying traffic, startup timeout, cancellation and restart rejection.

## [2026.5.2] - 2026-05-30

### Added

- Initial release: in-process Quick Tunnels (`trycloudflare.com`) and Named Tunnels (custom domains)
  driven through `github.com/cloudflare/cloudflared`.
- `Options` for `Name`, `Domain`, `OriginURL`, `ShowLog` and `Timeout`, plus `Tunnel` with `URL`,
  `Wait` and `Close`.
- Unit tests for option validation and tunnel lifecycle, and integration tests for Quick Tunnels.

[Unreleased]: https://github.com/luank3/flared/compare/v2026.5.2...HEAD
[2026.5.2]: https://github.com/luank3/flared/releases/tag/v2026.5.2
