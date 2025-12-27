package cloudflare

import (
	"testing"

	"schooner/internal/config"
)

func TestManager_IsConfigured(t *testing.T) {
	tests := []struct {
		name     string
		cfg      *config.Config
		expected bool
	}{
		{
			name: "configured with token and domain",
			cfg: &config.Config{
				Cloudflare: config.CloudflareConfig{
					TunnelToken: "test-token",
					Domain:      "example.com",
				},
			},
			expected: true,
		},
		{
			name: "not configured - missing token",
			cfg: &config.Config{
				Cloudflare: config.CloudflareConfig{
					TunnelToken: "",
					Domain:      "example.com",
				},
			},
			expected: false,
		},
		{
			name: "not configured - missing domain",
			cfg: &config.Config{
				Cloudflare: config.CloudflareConfig{
					TunnelToken: "test-token",
					Domain:      "",
				},
			},
			expected: false,
		},
		{
			name: "not configured - both empty",
			cfg: &config.Config{
				Cloudflare: config.CloudflareConfig{
					TunnelToken: "",
					Domain:      "",
				},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := NewManager(tt.cfg, nil)
			if got := m.IsConfigured(); got != tt.expected {
				t.Errorf("IsConfigured() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestIngressRule(t *testing.T) {
	rule := IngressRule{
		Hostname: "app.example.com",
		Service:  "http://localhost:8080",
	}

	if rule.Hostname != "app.example.com" {
		t.Errorf("Hostname = %v, want app.example.com", rule.Hostname)
	}
	if rule.Service != "http://localhost:8080" {
		t.Errorf("Service = %v, want http://localhost:8080", rule.Service)
	}
}

func TestTunnelConfig(t *testing.T) {
	cfg := TunnelConfig{
		Tunnel: "test-tunnel-id",
		Ingress: []IngressRule{
			{Hostname: "app1.example.com", Service: "http://localhost:8080"},
			{Hostname: "app2.example.com", Service: "http://localhost:9090"},
			{Service: "http_status:404"},
		},
	}

	if cfg.Tunnel != "test-tunnel-id" {
		t.Errorf("Tunnel = %v, want test-tunnel-id", cfg.Tunnel)
	}
	if len(cfg.Ingress) != 3 {
		t.Errorf("len(Ingress) = %v, want 3", len(cfg.Ingress))
	}
}
