# Security policy

## Supported versions

Only the latest tagged release (`vYYYY.M.PATCH` on `main`) is supported. Fixes are released as a new
tag rather than patched into older ones.

## Reporting a vulnerability

Please do not open a public issue for a security problem. Use GitHub's private vulnerability
reporting on this repository ("Security" tab → "Report a vulnerability"), or email the maintainer at
[lua_n@naver.com](mailto:lua_n@naver.com) if you cannot use GitHub.

Include what you have: affected version or commit, a description of the impact, and a minimal
reproduction if one exists. You can expect an acknowledgement within a few days and an assessment of
whether a fix is warranted.

## Scope

`flared` is a thin embedding layer: it provisions tunnels through Cloudflare's APIs and starts
cloudflared's runtime inside your process. Report in scope:

- Flaws in how `flared` handles credentials, hostnames or option input (for example, writing tunnel
  credentials somewhere unsafe, or logging secrets that `ShowLog` should not reveal).
- Ways to make `flared` take an unintended action on a Cloudflare account — creating, routing or
  repointing a DNS record that the caller did not ask for.
- Crashes, panics or denial of service reachable from the public `Options`/`Start` surface.

Out of scope here (report upstream at
[cloudflare/cloudflared](https://github.com/cloudflare/cloudflared/security)):

- The Cloudflare Tunnel protocol, edge behaviour, or `trycloudflare.com` availability.
- Vulnerabilities in cloudflared's own packages, `quic-go`, or the Cloudflare API.
- Anything that requires already having valid Cloudflare credentials with the privileges the code
  path needs.

## Handling secrets in reports

Never paste `cert.pem`, a tunnel credentials file, a tunnel token or an API token into an issue or
email. Describe the shape of the value instead, and redact identifiers that you do not want public.
