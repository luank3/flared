package flared

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/netip"
	"runtime"
	"sync"
	"time"

	"github.com/cloudflare/cloudflared/client"
	"github.com/cloudflare/cloudflared/config"
	"github.com/cloudflare/cloudflared/connection"
	"github.com/cloudflare/cloudflared/edgediscovery/allregions"
	"github.com/cloudflare/cloudflared/features"
	"github.com/cloudflare/cloudflared/ingress"
	"github.com/cloudflare/cloudflared/ingress/origins"
	"github.com/cloudflare/cloudflared/orchestration"
	"github.com/cloudflare/cloudflared/signal"
	"github.com/cloudflare/cloudflared/supervisor"
	"github.com/cloudflare/cloudflared/tlsconfig"
	"github.com/cloudflare/cloudflared/tunnelrpc/pogs"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/rs/zerolog"
)

// Defaults mirror cloudflared's own defaults for the flags that affect a locally run tunnel.
const (
	defaultTimeout                    = 15 * time.Second
	defaultHAConnections              = 4
	quickTunnelHAConnections          = 1
	defaultRetries                    = 5
	defaultMaxEdgeAddrRetries         = 8
	defaultRPCTimeout                 = 5 * time.Second
	defaultGracePeriod                = 30 * time.Second
	defaultQUICConnLevelFlowControl   = 30 << 20
	defaultQUICStreamLevelFlowControl = 6 << 20
)

// cloudflaredVersion identifies this process to the Cloudflare edge and API. The statically
// linked cloudflared version is not knowable at runtime, hence the placeholder.
const cloudflaredVersion = "unknown"

func userAgent() string { return "cloudflared/" + cloudflaredVersion }

func osArch() string { return runtime.GOOS + "_" + runtime.GOARCH }

// cloudflared's orchestrator, signal handling and Prometheus collectors are process-wide state, so
// only one runtime can exist per process. Guard against a second Start turning that into a panic.
var (
	runtimeMu     sync.Mutex
	runtimeActive bool
)

func acquireRuntime() error {
	runtimeMu.Lock()
	defer runtimeMu.Unlock()
	if runtimeActive {
		return fmt.Errorf("a tunnel is already running in this process: cloudflared's runtime can only be started once per process")
	}
	runtimeActive = true
	return nil
}

func releaseRuntime() {
	runtimeMu.Lock()
	defer runtimeMu.Unlock()
	runtimeActive = false
}

// runTunnel wires up cloudflared's runtime for an already provisioned tunnel and waits until it is
// connected to the edge, or until the startup timeout expires.
func runTunnel(ctx context.Context, opts Options, props *connection.TunnelProperties, url string, log *zerolog.Logger, budget time.Duration) (*Tunnel, error) {
	tCtx, cancel := context.WithCancel(ctx)
	t := &Tunnel{
		url:      url,
		cancel:   cancel,
		shutdown: make(chan struct{}),
	}

	tunnelConfig, orchestratorConfig, err := buildRuntimeConfig(tCtx, opts, props, log)
	if err != nil {
		cancel()
		releaseRuntime()
		return nil, err
	}

	orchestrator, err := orchestration.NewOrchestrator(tCtx, orchestratorConfig, tunnelConfig.Tags, nil, log)
	if err != nil {
		cancel()
		releaseRuntime()
		return nil, fmt.Errorf("failed to create orchestrator: %w", err)
	}

	connectedSignal := signal.New(make(chan struct{}))
	// The daemon reports the error it terminated with, so startup failures surface from Start
	// instead of being discovered later through Wait.
	exitCh := make(chan error, 1)
	t.wg.Add(1)
	go func() {
		defer t.wg.Done()
		err := supervisor.StartTunnelDaemon(tCtx, tunnelConfig, orchestrator, connectedSignal, t.shutdown)
		if tCtx.Err() == nil {
			t.err = err
		}
		releaseRuntime()
		exitCh <- t.err
	}()

	timer := time.NewTimer(budget)
	defer timer.Stop()

	select {
	case <-connectedSignal.Wait():
		log.Info().Msgf("Tunnel is ready at %s", url)
		return t, nil
	case err := <-exitCh:
		cancel()
		if err != nil {
			return nil, fmt.Errorf("tunnel failed to start: %w", err)
		}
		return nil, fmt.Errorf("tunnel exited before it connected to the Cloudflare edge")
	case <-timer.C:
		_ = t.Close()
		return nil, fmt.Errorf("timeout after %s waiting for the tunnel to connect to the Cloudflare edge", budget)
	case <-tCtx.Done():
		cancel()
		_ = t.Close()
		return nil, fmt.Errorf("tunnel startup cancelled: %w", ctx.Err())
	}
}

// buildRuntimeConfig assembles the cloudflared runtime configuration for a tunnel. Ingress is always
// a single rule forwarding every request to OriginURL, which is what cloudflared itself does when
// started with just --url.
func buildRuntimeConfig(ctx context.Context, opts Options, props *connection.TunnelProperties, log *zerolog.Logger) (*supervisor.TunnelConfig, *orchestration.Config, error) {
	featureSelector, err := features.NewFeatureSelector(ctx, props.Credentials.AccountTag, nil, false, log)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to determine account features: %w", err)
	}
	clientConfig, err := client.NewConfig(cloudflaredVersion, osArch(), featureSelector)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create client config: %w", err)
	}
	log.Info().Msgf("Generated Connector ID: %s", clientConfig.ConnectorID)

	ingressRules, err := ingress.ParseIngress(&config.Configuration{
		Ingress: []config.UnvalidatedIngressRule{{Service: opts.OriginURL}},
	})
	if err != nil {
		return nil, nil, fmt.Errorf("invalid OriginURL %q: %w", opts.OriginURL, err)
	}

	protocolSelector, err := connection.NewProtocolSelector(protocol(opts, props), log)
	if err != nil {
		return nil, nil, err
	}
	log.Info().Msgf("Initial protocol %s", protocolSelector.Current())

	edgeTLSConfigs, err := buildEdgeTLSConfigs()
	if err != nil {
		return nil, nil, err
	}

	warpRouting := ingress.NewWarpRoutingConfig(&config.WarpRoutingConfig{})
	originDialer := ingress.NewOriginDialer(ingress.OriginConfig{
		DefaultDialer: ingress.NewDialer(warpRouting),
	}, log)
	dnsResolver := origins.NewDNSResolverService(origins.NewDNSDialer(), log, origins.NewMetrics(prometheus.DefaultRegisterer))
	originDialer.AddReservedService(dnsResolver, []netip.AddrPort{origins.VirtualDNSServiceAddr})

	haConnections := defaultHAConnections
	if props.QuickTunnelUrl != "" {
		// Quick Tunnels are not for production use, so a single connection is enough.
		haConnections = quickTunnelHAConnections
	}

	tunnelConfig := &supervisor.TunnelConfig{
		ClientConfig:                        clientConfig,
		GracePeriod:                         defaultGracePeriod,
		Region:                              props.Credentials.Endpoint,
		EdgeIPVersion:                       allregions.Auto,
		HAConnections:                       haConnections,
		Tags:                                []pogs.Tag{{Name: "ID", Value: clientConfig.ConnectorID.String()}},
		Log:                                 log,
		Observer:                            connection.NewObserver(log),
		ReportedVersion:                     cloudflaredVersion,
		Retries:                             defaultRetries,
		MaxEdgeAddrRetries:                  defaultMaxEdgeAddrRetries,
		NamedTunnel:                         props,
		ProtocolSelector:                    protocolSelector,
		EdgeTLSConfigs:                      edgeTLSConfigs,
		RPCTimeout:                          defaultRPCTimeout,
		QUICConnectionLevelFlowControlLimit: defaultQUICConnLevelFlowControl,
		QUICStreamLevelFlowControlLimit:     defaultQUICStreamLevelFlowControl,
		OriginDNSService:                    dnsResolver,
		OriginDialerService:                 originDialer,
	}

	orchestratorConfig := &orchestration.Config{
		Ingress:             &ingressRules,
		WarpRouting:         warpRouting,
		OriginDialerService: originDialer,
	}
	return tunnelConfig, orchestratorConfig, nil
}

// protocol returns the edge transport to use, defaulting the way cloudflared does: QUIC for Quick
// Tunnels, and "auto" (QUIC with an HTTP/2 fallback) for Named Tunnels.
func protocol(opts Options, props *connection.TunnelProperties) string {
	if opts.Protocol != "" {
		return opts.Protocol
	}
	if props.QuickTunnelUrl != "" {
		return connection.QUIC.String()
	}
	return connection.AutoSelectFlag
}

// buildEdgeTLSConfigs builds the TLS configuration used for every edge protocol we can connect with.
func buildEdgeTLSConfigs() (map[connection.Protocol]*tls.Config, error) {
	configs := make(map[connection.Protocol]*tls.Config, len(connection.ProtocolList))
	for _, p := range connection.ProtocolList {
		settings := p.TLSSettings()
		if settings == nil {
			return nil, fmt.Errorf("%s has unknown TLS settings", p)
		}
		tlsConfig, err := tlsconfig.CreateTunnelConfig("", settings.ServerName)
		if err != nil {
			return nil, fmt.Errorf("failed to create TLS config for %s: %w", p, err)
		}
		if len(settings.NextProtos) > 0 {
			tlsConfig.NextProtos = settings.NextProtos
		}
		configs[p] = tlsConfig
	}
	return configs, nil
}
