package flared

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/cloudflare/cloudflared/connection"
	"github.com/google/uuid"
)

func TestValidateOptions_MissingOriginURL(t *testing.T) {
	err := validateOptions(Options{})
	if err == nil {
		t.Fatal("expected error for missing OriginURL")
	}
	if !strings.Contains(err.Error(), "OriginURL is required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateOptions_NameWithoutDomain(t *testing.T) {
	err := validateOptions(Options{
		OriginURL: "http://localhost:8080",
		Name:      "my-tunnel",
	})
	if err == nil {
		t.Fatal("expected error when Name is set without Domain")
	}
	if !strings.Contains(err.Error(), "both Name and Domain must be provided together") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateOptions_DomainWithoutName(t *testing.T) {
	err := validateOptions(Options{
		OriginURL: "http://localhost:8080",
		Domain:    "example.com",
	})
	if err == nil {
		t.Fatal("expected error when Domain is set without Name")
	}
	if !strings.Contains(err.Error(), "both Name and Domain must be provided together") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateOptions_ValidQuickTunnel(t *testing.T) {
	err := validateOptions(Options{OriginURL: "http://localhost:8080"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateOptions_ValidNamedTunnel(t *testing.T) {
	err := validateOptions(Options{
		OriginURL: "http://localhost:8080",
		Name:      "my-tunnel",
		Domain:    "example.com",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestTunnel_URL(t *testing.T) {
	tun := &Tunnel{url: "https://abc.trycloudflare.com"}
	if got := tun.URL(); got != "https://abc.trycloudflare.com" {
		t.Fatalf("URL() = %q, want %q", got, "https://abc.trycloudflare.com")
	}
}

func TestTunnel_URL_Empty(t *testing.T) {
	tun := &Tunnel{}
	if got := tun.URL(); got != "" {
		t.Fatalf("URL() = %q, want empty", got)
	}
}

func TestTunnel_Close_Idempotent(t *testing.T) {
	_, cancel := context.WithCancel(context.Background())
	tun := &Tunnel{
		cancel:   cancel,
		shutdown: make(chan struct{}),
	}
	tun.wg.Add(1)
	go func() {
		defer tun.wg.Done()
		<-tun.shutdown
	}()

	if err := tun.Close(); err != nil {
		t.Fatalf("first Close() error: %v", err)
	}
	if err := tun.Close(); err != nil {
		t.Fatalf("second Close() error: %v", err)
	}
}

func TestTunnel_Wait_NoError(t *testing.T) {
	_, cancel := context.WithCancel(context.Background())
	tun := &Tunnel{
		cancel:   cancel,
		shutdown: make(chan struct{}),
	}
	tun.wg.Add(1)
	go func() {
		defer tun.wg.Done()
		<-tun.shutdown
	}()

	tun.Close()
	if err := tun.Wait(); err != nil {
		t.Fatalf("Wait() error = %v, want nil", err)
	}
}

func TestTunnel_Wait_WithError(t *testing.T) {
	_, cancel := context.WithCancel(context.Background())
	tun := &Tunnel{
		cancel:   cancel,
		shutdown: make(chan struct{}),
	}
	tun.wg.Add(1)
	go func() {
		defer tun.wg.Done()
		tun.err = context.Canceled
	}()

	if err := tun.Wait(); err != context.Canceled {
		t.Fatalf("Wait() error = %v, want %v", err, context.Canceled)
	}
}

func TestNormalizeHostname(t *testing.T) {
	tests := []struct {
		domain  string
		want    string
		wantErr bool
	}{
		{domain: "app.example.com", want: "app.example.com"},
		{domain: "https://app.example.com", want: "app.example.com"},
		{domain: "http://app.example.com/", want: "app.example.com"},
		{domain: "app.example.com:8443", wantErr: true},
		{domain: "", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.domain, func(t *testing.T) {
			got, err := normalizeHostname(tt.domain)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("normalizeHostname(%q) = %q, want error", tt.domain, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("normalizeHostname(%q) error: %v", tt.domain, err)
			}
			if got != tt.want {
				t.Fatalf("normalizeHostname(%q) = %q, want %q", tt.domain, got, tt.want)
			}
		})
	}
}

func TestProtocol(t *testing.T) {
	quick := &connection.TunnelProperties{Credentials: connection.Credentials{TunnelID: uuid.New()}, QuickTunnelUrl: "x.trycloudflare.com"}
	named := &connection.TunnelProperties{}

	tests := []struct {
		name  string
		opts  Options
		props *connection.TunnelProperties
		want  string
	}{
		{name: "explicit protocol wins", opts: Options{Protocol: "http2"}, props: quick, want: "http2"},
		{name: "quick tunnel defaults to quic", props: quick, want: "quic"},
		{name: "named tunnel defaults to auto", props: named, want: connection.AutoSelectFlag},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := protocol(tt.opts, tt.props); got != tt.want {
				t.Fatalf("protocol() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestNewLogger_Suppressed(t *testing.T) {
	var buf bytes.Buffer
	log := newLogger(Options{ShowLog: false, LogWriter: &buf})
	log.Info().Msg("hidden")
	if buf.Len() != 0 {
		t.Fatalf("expected no log output, got %q", buf.String())
	}
}

func TestNewLogger_WritesToWriter(t *testing.T) {
	var buf bytes.Buffer
	log := newLogger(Options{ShowLog: true, LogWriter: &buf})
	log.Info().Msg("visible")
	if !strings.Contains(buf.String(), "visible") {
		t.Fatalf("expected log output to contain message, got %q", buf.String())
	}
}

func TestValidateOptions_InvalidOriginURL(t *testing.T) {
	err := validateOptions(Options{OriginURL: "not-a-url"})
	if err == nil {
		t.Fatal("expected error for an OriginURL without a scheme and host")
	}
	if !strings.Contains(err.Error(), "invalid OriginURL") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateOptions_InvalidProtocol(t *testing.T) {
	err := validateOptions(Options{OriginURL: "http://localhost:8080", Protocol: "bogus"})
	if err == nil {
		t.Fatal("expected error for an unknown protocol")
	}
	if !strings.Contains(err.Error(), "unknown protocol") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateOptions_ValidProtocol(t *testing.T) {
	for _, protocol := range []string{"quic", "http2", "auto"} {
		if err := validateOptions(Options{OriginURL: "http://localhost:8080", Protocol: protocol}); err != nil {
			t.Fatalf("validateOptions(Protocol: %q) error: %v", protocol, err)
		}
	}
}

func TestAcquireRuntime(t *testing.T) {
	resetRuntimeState()
	t.Cleanup(resetRuntimeState)

	if err := acquireRuntime(); err != nil {
		t.Fatalf("first acquireRuntime() error: %v", err)
	}
	if err := acquireRuntime(); err == nil {
		t.Fatal("expected a second tunnel in the same process to be rejected")
	}

	releaseRuntime()
	if err := acquireRuntime(); err != nil {
		t.Fatalf("acquireRuntime() after release error: %v", err)
	}

	// Collectors registered by a started runtime are never unregistered, so once a runtime has been
	// built the process cannot run another tunnel, even after the first one is closed.
	releaseRuntime()
	markRuntimeUsed()
	err := acquireRuntime()
	if err == nil {
		t.Fatal("expected a second runtime in the same process to be rejected")
	}
	if !strings.Contains(err.Error(), "cannot be restarted") {
		t.Fatalf("unexpected error: %v", err)
	}
}
