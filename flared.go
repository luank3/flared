// Package flared runs Cloudflare Tunnels (cloudflared) in-process.
package flared

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/cloudflare/cloudflared/validation"
	"github.com/rs/zerolog"
)

// Options contains the configuration for starting a cloudflared tunnel.
type Options struct {
	// Name is the stable name to identify the tunnel. Used along with Domain to create, route, and run a Named Tunnel.
	// Requires cloudflare credentials (cert.pem) to be present; if they are missing a browser login is started.
	Name string
	// Domain is the expected public hostname for Named Tunnels (e.g., "app.example.com").
	// If provided alongside Name, it will be used to route traffic.
	Domain string
	// OverwriteDNS makes routing a Named Tunnel replace an existing DNS record for Domain instead of
	// failing when that record points somewhere else. Records that already route to the tunnel are left as is.
	OverwriteDNS bool
	// OriginURL is the local service URL to expose (e.g. "http://127.0.0.1:8080").
	OriginURL string
	// Protocol is the transport used to reach the Cloudflare edge: "quic", "http2" or "auto".
	// "auto" starts with QUIC and falls back to HTTP/2. Defaults to "auto" for Named Tunnels and
	// "quic" for Quick Tunnels, matching cloudflared.
	Protocol string
	// ShowLog determines whether cloudflared's internal logs should be printed to os.Stderr.
	ShowLog bool
	// LogWriter is an optional writer to receive cloudflared's internal logs.
	// If nil and ShowLog is false, logs are suppressed.
	LogWriter io.Writer
	// Timeout is the maximum time Start may take to provision the tunnel and connect it to the
	// Cloudflare edge. It does not limit the lifetime of the returned Tunnel.
	// Defaults to 15 seconds if zero.
	Timeout time.Duration
}

// Tunnel represents an active, in-process Cloudflare Tunnel.
type Tunnel struct {
	url       string
	cancel    context.CancelFunc
	shutdown  chan struct{}
	err       error
	wg        sync.WaitGroup
	closeOnce sync.Once
}

// URL returns the public URL where the tunnel is accessible.
// For Quick Tunnels, this is the generated trycloudflare.com address.
// For Named Tunnels, this is the Domain provided in Options.
func (t *Tunnel) URL() string {
	return t.url
}

// Close gracefully shuts down the tunnel and stops the tunnel connections.
func (t *Tunnel) Close() error {
	t.closeOnce.Do(func() {
		t.cancel()
		close(t.shutdown)
	})
	t.wg.Wait()
	return nil
}

// Wait blocks until the tunnel is closed or encounters a fatal error.
func (t *Tunnel) Wait() error {
	t.wg.Wait()
	return t.err
}

// validateOptions checks that the given options are valid before starting a tunnel.
func validateOptions(opts Options) error {
	if opts.OriginURL == "" {
		return fmt.Errorf("OriginURL is required")
	}
	if (opts.Name != "" && opts.Domain == "") || (opts.Name == "" && opts.Domain != "") {
		return fmt.Errorf("both Name and Domain must be provided together for Named Tunnels")
	}
	// Reject an unusable OriginURL or Protocol here: provisioning a Named Tunnel creates a tunnel
	// and a DNS route, which must not happen for options that cannot run.
	if _, err := ingressForOrigin(opts.OriginURL); err != nil {
		return fmt.Errorf("invalid OriginURL %q: %w", opts.OriginURL, err)
	}
	if err := validateProtocol(opts.Protocol); err != nil {
		return err
	}
	return nil
}

// Start creates and runs a tunnel in-process. It blocks until the tunnel is connected to the edge.
func Start(ctx context.Context, opts Options) (*Tunnel, error) {
	if err := validateOptions(opts); err != nil {
		return nil, err
	}
	if ctx == nil {
		ctx = context.Background()
	}

	if err := acquireRuntime(); err != nil {
		return nil, err
	}

	log := newLogger(opts)

	// The startup budget covers provisioning and connecting; the returned Tunnel is not bound by it.
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	deadline := time.Now().Add(timeout)
	provisionCtx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()

	props, url, err := provision(provisionCtx, opts, log)
	if err != nil {
		releaseRuntime()
		if errors.Is(err, context.DeadlineExceeded) {
			return nil, fmt.Errorf("timeout after %s provisioning the tunnel: %w", timeout, err)
		}
		return nil, err
	}

	return runTunnel(ctx, opts, props, url, log, deadline, timeout)
}

// newLogger builds the zerolog logger handed to cloudflared. Logs are only produced when ShowLog
// is set, so nothing is ever written to the process's stderr behind the caller's back.
func newLogger(opts Options) *zerolog.Logger {
	if !opts.ShowLog {
		logger := zerolog.Nop()
		return &logger
	}
	writer := opts.LogWriter
	if writer == nil {
		writer = os.Stderr
	}
	logger := zerolog.New(writer).Level(zerolog.InfoLevel).With().Timestamp().Logger()
	return &logger
}

// normalizeHostname strips any scheme from a domain and validates the remaining hostname. Ports and
// paths are rejected rather than silently dropped, because DNS routes are created for bare hostnames.
func normalizeHostname(domain string) (string, error) {
	hostname := strings.TrimPrefix(strings.TrimPrefix(domain, "https://"), "http://")
	hostname = strings.TrimSuffix(hostname, "/")
	if hostname == "" || strings.ContainsAny(hostname, ":/") {
		return "", fmt.Errorf("invalid Domain %q: expected a bare hostname such as app.example.com", domain)
	}
	hostname, err := validation.ValidateHostname(hostname)
	if err != nil {
		return "", fmt.Errorf("invalid Domain %q: %w", domain, err)
	}
	return hostname, nil
}
