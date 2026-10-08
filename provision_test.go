package flared

import (
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestParseQuickTunnelResponse_Success(t *testing.T) {
	body := []byte(`{
		"success": true,
		"result": {
			"id": "a3f1c0d2-4b5e-4f6a-8b7c-9d0e1f2a3b4c",
			"name": "quick-tunnel",
			"hostname": "random-words.trycloudflare.com",
			"account_tag": "account",
			"secret": "c2VjcmV0"
		},
		"errors": []
	}`)

	credentials, hostname, err := parseQuickTunnelResponse(200, body)
	if err != nil {
		t.Fatalf("parseQuickTunnelResponse() error: %v", err)
	}
	if hostname != "random-words.trycloudflare.com" {
		t.Fatalf("hostname = %q", hostname)
	}
	if credentials.AccountTag != "account" {
		t.Fatalf("AccountTag = %q", credentials.AccountTag)
	}
	if string(credentials.TunnelSecret) != "secret" {
		t.Fatalf("TunnelSecret = %q", credentials.TunnelSecret)
	}
	if credentials.TunnelID.String() != "a3f1c0d2-4b5e-4f6a-8b7c-9d0e1f2a3b4c" {
		t.Fatalf("TunnelID = %s", credentials.TunnelID)
	}
}

func TestParseQuickTunnelResponse_Errors(t *testing.T) {
	tests := []struct {
		name    string
		status  int
		body    string
		wantErr string
	}{
		{
			name:    "api errors",
			status:  400,
			body:    `{"success":false,"errors":[{"code":1000,"message":"bad request"}]}`,
			wantErr: "1000: bad request",
		},
		{
			name:    "unsuccessful",
			status:  500,
			body:    `{"success":false,"errors":[]}`,
			wantErr: "status 500",
		},
		{
			name:    "malformed json",
			status:  200,
			body:    `not json`,
			wantErr: "failed to unmarshal",
		},
		{
			name:    "missing hostname",
			status:  200,
			body:    `{"success":true,"result":{"id":"a3f1c0d2-4b5e-4f6a-8b7c-9d0e1f2a3b4c"}}`,
			wantErr: "no hostname",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := parseQuickTunnelResponse(tt.status, []byte(tt.body))
			if err == nil {
				t.Fatalf("expected error containing %q", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error %q does not contain %q", err.Error(), tt.wantErr)
			}
		})
	}
}

func TestTunnelCredentialsPath(t *testing.T) {
	id := "a3f1c0d2-4b5e-4f6a-8b7c-9d0e1f2a3b4c"
	path := tunnelCredentialsPath("/home/user/.cloudflared", uuid.MustParse(id))
	if want := "/home/user/.cloudflared/" + id + ".json"; path != want {
		t.Fatalf("tunnelCredentialsPath() = %q, want %q", path, want)
	}
}
