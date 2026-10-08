# flared

`flared` is a Go library that allows you to run Cloudflare Tunnels (`cloudflared`) directly from your Go code.

It supports both **Quick Tunnels** (temporary tunnels via `trycloudflare.com`) and **Named Tunnels** (custom domains via Cloudflare DNS), over QUIC or HTTP/2.

## Installation

```bash
go get github.com/lucanhost/flared
```

## Quick Start

### Quick Tunnel (trycloudflare.com)
Creates a temporary, random URL to expose your local service.

```go
package main

import (
	"context"
	"fmt"
	"log"
	"net/http"

	"github.com/lucanhost/flared"
)

func main() {
	// 1. Start a simple HTTP server
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "Welcome to the Go HTTP Server (Quick Tunnel)!")
	})
	go func() {
		if err := http.ListenAndServe(":8080", nil); err != nil {
			log.Fatal(err)
		}
	}()

	// 2. Start Cloudflare Tunnel
	opts := flared.Options{
		OriginURL: "http://localhost:8080", // Local URL to expose
		ShowLog:   false,                   // Toggle cloudflared internal logs
	}

	tunnel, err := flared.Start(context.Background(), opts)
	if err != nil {
		log.Fatal(err)
	}
	defer tunnel.Close()

	fmt.Printf("Tunnel URL: %s\n", tunnel.URL())
	tunnel.Wait()
}
```

### Named Tunnel (Custom Domain)
Creates and routes a stable tunnel to your custom domain. Requires a valid `cert.pem` on the host; if it is missing, the library starts the `cloudflared` browser login flow and stores the certificate itself. The tunnel is created if it does not exist yet, and `Domain` is routed to it — a routing failure fails `Start` rather than leaving the tunnel publicly unreachable.

Set `OverwriteDNS` only when you intend to repoint an existing DNS record that currently serves something else.

```go
package main

import (
	"context"
	"fmt"
	"log"
	"net/http"

	"github.com/lucanhost/flared"
)

func main() {
	// 1. Start a simple HTTP server
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "Welcome to the Go HTTP Server (Named Tunnel)!")
	})
	go func() {
		if err := http.ListenAndServe(":8080", nil); err != nil {
			log.Fatal(err)
		}
	}()

	// 2. Start Cloudflare Tunnel
	opts := flared.Options{
		OriginURL: "http://localhost:8080",
		Name:      "my-demo-tunnel",
		Domain:    "api.example.com",
		// OverwriteDNS repoints an existing DNS record for Domain. Leave it false unless you
		// really want to move a hostname that already serves traffic somewhere else.
		OverwriteDNS: false,
		ShowLog:      false,
	}

	tunnel, err := flared.Start(context.Background(), opts)
	if err != nil {
		log.Fatal(err)
	}
	defer tunnel.Close()

	fmt.Printf("Tunnel URL: %s\n", tunnel.URL())
	tunnel.Wait()
}
```

## How it works

Under the hood, `flared` imports the official `github.com/cloudflare/cloudflared` module and drives its runtime directly: it provisions the tunnel, builds the supervisor/orchestrator configuration, and connects to the Cloudflare edge in your process. Nothing is shelled out, no CLI argument parsing is involved, and the process's `stderr` is never redirected.

Quick Tunnel URLs come from the provisioning API response rather than from parsed log output, and `Start` only returns once a connection to the edge is established — so a returned `*Tunnel` is ready to serve traffic.

> [!NOTE]
> `cloudflared`'s runtime (orchestrator, signal handling and Prometheus collectors) is process-wide state, so one process can run **one tunnel at a time** in this version. A second `Start` before `Close` returns an error.

> [!WARNING]
> **Binary Size Impact**
> Because `flared` statically compiles the entire `cloudflared` core into your application, your final compiled binary size will increase by approximately **30-40 MB**. This is the expected trade-off for achieving a seamless, zero-dependency deployment for your users.

## Configuration (Options)

- `OriginURL` (string): The local service URL to expose (e.g., `http://localhost:8080`). **(Required)**
- `Name` (string): The stable name of the tunnel. Used alongside `Domain` to create and route a Named Tunnel.
- `Domain` (string): The public hostname to route traffic through (e.g., `api.example.com`).
- `OverwriteDNS` (bool): When routing a Named Tunnel, replace an existing DNS record for `Domain` that points somewhere else. Defaults to `false`, which fails loudly instead of silently repointing a record.
- `Protocol` (string): Edge transport, one of `quic`, `http2` or `auto`. `auto` starts with QUIC and falls back to HTTP/2. Defaults to `auto` for Named Tunnels and `quic` for Quick Tunnels, as `cloudflared` does.
- `ShowLog` (bool): Toggles the output of internal `cloudflared` logs to `os.Stderr`.
- `LogWriter` (io.Writer): Alternative destination for internal logs. Ignored unless `ShowLog` is set.
- `Timeout` (time.Duration): How long `Start` may take to provision the tunnel and connect it to the edge. It does not limit the lifetime of the returned `Tunnel`. Defaults to 15 seconds.

*Note: If both `Name` and `Domain` are omitted, the library defaults to creating a Quick Tunnel.*

## Dependencies

`flared` requires no `replace` directives in your `go.mod`; the upstream `cloudflared`, `quic-go` and `urfave/cli` modules are used as published.
