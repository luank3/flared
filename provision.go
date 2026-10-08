package flared

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/cloudflare/cloudflared/cfapi"
	"github.com/cloudflare/cloudflared/config"
	"github.com/cloudflare/cloudflared/connection"
	"github.com/cloudflare/cloudflared/credentials"
	"github.com/cloudflare/cloudflared/token"
	"github.com/google/uuid"
	homedir "github.com/mitchellh/go-homedir"
	"github.com/rs/zerolog"
)

const (
	// quickTunnelService is the endpoint provisioning account-less Tunnels.
	quickTunnelService = "https://api.trycloudflare.com"
	// quickTunnelResponseLimit caps how much of a provisioning response we are willing to buffer.
	quickTunnelResponseLimit = 1 << 20
	// apiURL is the base URL of the Cloudflare API used to create, find and route Named Tunnels.
	apiURL = "https://api.cloudflare.com/client/v4"
	// loginURL and callbackURL are the endpoints of the browser based login flow.
	loginURL    = "https://dash.cloudflare.com/argotunnel"
	callbackURL = "https://login.cloudflareaccess.org/"
)

// provision returns everything needed to run the requested tunnel: its credentials and public URL.
func provision(ctx context.Context, opts Options, log *zerolog.Logger) (*connection.TunnelProperties, string, error) {
	if opts.Name == "" {
		return provisionQuickTunnel(ctx, opts, log)
	}
	return provisionNamedTunnel(ctx, opts, log)
}

type quickTunnelResponse struct {
	Success bool             `json:"success"`
	Result  quickTunnel      `json:"result"`
	Errors  []quickTunnelErr `json:"errors"`
}

type quickTunnel struct {
	ID         string `json:"id"`
	Hostname   string `json:"hostname"`
	AccountTag string `json:"account_tag"`
	Secret     []byte `json:"secret"`
}

type quickTunnelErr struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// provisionQuickTunnel asks the quick Tunnel service for a tunnel on trycloudflare.com. The
// hostname comes straight from the provisioning response, so no log output has to be parsed.
func provisionQuickTunnel(ctx context.Context, opts Options, log *zerolog.Logger) (*connection.TunnelProperties, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, quickTunnelService+"/tunnel", nil)
	if err != nil {
		return nil, "", fmt.Errorf("failed to build quick Tunnel request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", userAgent())

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("failed to request quick Tunnel: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, quickTunnelResponseLimit))
	if err != nil {
		return nil, "", fmt.Errorf("failed to read quick Tunnel response: %w", err)
	}

	tunnelCredentials, hostname, err := parseQuickTunnelResponse(resp.StatusCode, body)
	if err != nil {
		return nil, "", err
	}

	quickTunnelURL := hostname
	if !strings.HasPrefix(quickTunnelURL, "https://") {
		quickTunnelURL = "https://" + quickTunnelURL
	}
	log.Info().Msgf("Your quick Tunnel has been created! Visit it at (it may take some time to be reachable): %s", quickTunnelURL)

	return &connection.TunnelProperties{
		Credentials:    tunnelCredentials,
		QuickTunnelUrl: hostname,
	}, quickTunnelURL, nil
}

// parseQuickTunnelResponse turns a provisioning response into tunnel credentials and a hostname.
func parseQuickTunnelResponse(status int, body []byte) (connection.Credentials, string, error) {
	var data quickTunnelResponse
	if err := json.Unmarshal(body, &data); err != nil {
		return connection.Credentials{}, "", fmt.Errorf("failed to unmarshal quick Tunnel response (status %d): %w", status, err)
	}
	if len(data.Errors) > 0 {
		return connection.Credentials{}, "", fmt.Errorf("quick Tunnel provisioning failed (status %d): %s", status, quickTunnelErrors(data.Errors))
	}
	if status != http.StatusOK || !data.Success {
		return connection.Credentials{}, "", fmt.Errorf("quick Tunnel provisioning failed with status %d", status)
	}
	tunnelID, err := uuid.Parse(data.Result.ID)
	if err != nil {
		return connection.Credentials{}, "", fmt.Errorf("failed to parse quick Tunnel ID: %w", err)
	}
	if data.Result.Hostname == "" {
		return connection.Credentials{}, "", fmt.Errorf("quick Tunnel provisioning returned no hostname")
	}

	tunnelCredentials := connection.Credentials{
		AccountTag:   data.Result.AccountTag,
		TunnelSecret: data.Result.Secret,
		TunnelID:     tunnelID,
	}
	return tunnelCredentials, data.Result.Hostname, nil
}

func quickTunnelErrors(errs []quickTunnelErr) string {
	messages := make([]string, 0, len(errs))
	for _, e := range errs {
		messages = append(messages, fmt.Sprintf("%d: %s", e.Code, e.Message))
	}
	return strings.Join(messages, "; ")
}

// provisionNamedTunnel creates (or reuses) the tunnel named in Options, routes Domain to it, and
// returns the credentials needed to run it.
func provisionNamedTunnel(ctx context.Context, opts Options, log *zerolog.Logger) (*connection.TunnelProperties, string, error) {
	hostname, err := normalizeHostname(opts.Domain)
	if err != nil {
		return nil, "", err
	}

	certPath, err := ensureCert(ctx, log)
	if err != nil {
		return nil, "", err
	}
	user, err := credentials.Read(certPath, log)
	if err != nil {
		return nil, "", err
	}

	endpoint := apiURL
	if user.IsFEDEndpoint() {
		endpoint = credentials.FedRampBaseApiURL
	}
	client, err := user.Client(endpoint, userAgent(), log)
	if err != nil {
		return nil, "", fmt.Errorf("failed to create Cloudflare API client: %w", err)
	}

	tunnelID, tunnelCredentials, err := findOrCreateTunnel(client, user, filepath.Dir(certPath), opts.Name, log)
	if err != nil {
		return nil, "", err
	}

	result, err := client.RouteTunnel(tunnelID, cfapi.NewDNSRoute(hostname, opts.OverwriteDNS))
	if err != nil {
		return nil, "", fmt.Errorf("failed to route %s to tunnel %s: %w", hostname, tunnelID, err)
	}
	log.Info().Msg(result.SuccessSummary())

	return &connection.TunnelProperties{Credentials: tunnelCredentials}, "https://" + hostname, nil
}

// findOrCreateTunnel returns the credentials of the tunnel called name, creating the tunnel (and
// writing its credentials file next to cert.pem) when it does not exist yet.
func findOrCreateTunnel(client cfapi.Client, user *credentials.User, certDir, name string, log *zerolog.Logger) (uuid.UUID, connection.Credentials, error) {
	filter := cfapi.NewTunnelFilter()
	filter.NoDeleted()
	filter.ByName(name)

	tunnels, err := client.ListTunnels(filter)
	if err != nil {
		return uuid.Nil, connection.Credentials{}, fmt.Errorf("failed to look up tunnel %s: %w", name, err)
	}
	if len(tunnels) > 0 {
		tunnelID := tunnels[0].ID
		log.Info().Msgf("Reusing existing tunnel %s with ID %s", name, tunnelID)

		tunnelCredentials, err := readTunnelCredentials(tunnelCredentialsPath(certDir, tunnelID))
		if err != nil {
			return uuid.Nil, connection.Credentials{}, err
		}
		return tunnelID, tunnelCredentials, nil
	}

	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return uuid.Nil, connection.Credentials{}, fmt.Errorf("failed to generate a secret for tunnel %s: %w", name, err)
	}
	tunnel, err := client.CreateTunnel(name, secret)
	if err != nil {
		return uuid.Nil, connection.Credentials{}, fmt.Errorf("failed to create tunnel %s: %w", name, err)
	}

	tunnelCredentials := connection.Credentials{
		AccountTag:   user.AccountID(),
		TunnelSecret: secret,
		TunnelID:     tunnel.ID,
		Endpoint:     user.Endpoint(),
	}
	credentialsPath := tunnelCredentialsPath(certDir, tunnel.ID)
	body, err := json.Marshal(&tunnelCredentials)
	if err != nil {
		return uuid.Nil, connection.Credentials{}, fmt.Errorf("failed to marshal credentials for tunnel %s: %w", tunnel.ID, err)
	}
	if err := os.WriteFile(credentialsPath, body, 0o400); err != nil {
		return uuid.Nil, connection.Credentials{}, fmt.Errorf("tunnel %s was created with ID %s but its credentials could not be written to %s: %w", name, tunnel.ID, credentialsPath, err)
	}
	log.Info().Msgf("Created tunnel %s with ID %s", name, tunnel.ID)

	return tunnel.ID, tunnelCredentials, nil
}

// readTunnelCredentials reads the credentials file written by cloudflared for a tunnel.
func readTunnelCredentials(path string) (connection.Credentials, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return connection.Credentials{}, fmt.Errorf("failed to read tunnel credentials from %s: %w", path, err)
	}
	var tunnelCredentials connection.Credentials
	if err := json.Unmarshal(body, &tunnelCredentials); err != nil {
		return connection.Credentials{}, fmt.Errorf("failed to parse tunnel credentials from %s: %w", path, err)
	}
	return tunnelCredentials, nil
}

func tunnelCredentialsPath(directory string, tunnelID uuid.UUID) string {
	return filepath.Join(directory, tunnelID.String()+".json")
}

// ensureCert returns the path of the origin certificate, running cloudflared's browser login flow
// when no certificate is present yet.
func ensureCert(ctx context.Context, log *zerolog.Logger) (string, error) {
	if certPath := credentials.FindDefaultOriginCertPath(); certPath != "" {
		return certPath, nil
	}

	configDir, err := homedir.Expand(config.DefaultConfigSearchDirectories()[0])
	if err != nil {
		return "", fmt.Errorf("failed to locate cloudflared's config directory: %w", err)
	}
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		return "", fmt.Errorf("failed to create cloudflared's config directory: %w", err)
	}
	certPath := filepath.Join(configDir, credentials.DefaultCredentialFile)

	log.Info().Msg("cert.pem not found. Starting Cloudflare login process...")
	loginEndpoint, err := url.Parse(loginURL)
	if err != nil {
		return "", fmt.Errorf("failed to parse login URL: %w", err)
	}

	type loginResult struct {
		cert []byte
		err  error
	}
	login := make(chan loginResult, 1)
	go func() {
		cert, err := token.RunTransfer(loginEndpoint, "", "cert", "callback", callbackURL, false, false, false, false, log, "")
		login <- loginResult{cert: cert, err: err}
	}()

	var certData []byte
	select {
	case result := <-login:
		if result.err != nil {
			return "", fmt.Errorf("cloudflared login failed: %w", result.err)
		}
		certData = result.cert
	case <-ctx.Done():
		return "", fmt.Errorf("cloudflared login cancelled: %w", ctx.Err())
	}

	cert, err := credentials.DecodeOriginCert(certData)
	if err != nil {
		return "", fmt.Errorf("failed to decode origin certificate: %w", err)
	}
	encoded, err := cert.EncodeOriginCert()
	if err != nil {
		return "", fmt.Errorf("failed to encode origin certificate: %w", err)
	}
	if err := os.WriteFile(certPath, encoded, 0o600); err != nil {
		return "", fmt.Errorf("failed to write origin certificate to %s: %w", certPath, err)
	}
	log.Info().Msgf("You have successfully logged in. Credentials saved to %s", certPath)

	return certPath, nil
}
