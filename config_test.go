package main

import (
	"os"
	"path/filepath"
	"testing"
)

const validTOML = `
required_tags = ["publish:yes"]
required_suffix = ".example.com"
trim_suffix = "example.com"
append_suffix = "nebula.example.com"
prune = "network"

[cloudflare]
api_token = "cf-token"
zone_name = "example.com"

[definednet]
api_token = "dn-token"
network_id = "net-123"
`

func writeConfigFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadConfig_FromFile(t *testing.T) {
	path := writeConfigFile(t, validTOML)

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Cloudflare.APIToken != "cf-token" {
		t.Errorf("Cloudflare.APIToken = %q, want %q", cfg.Cloudflare.APIToken, "cf-token")
	}
	if cfg.Cloudflare.ZoneName != "example.com" {
		t.Errorf("Cloudflare.ZoneName = %q, want %q", cfg.Cloudflare.ZoneName, "example.com")
	}
	if cfg.DefinedNet.APIToken != "dn-token" {
		t.Errorf("DefinedNet.APIToken = %q, want %q", cfg.DefinedNet.APIToken, "dn-token")
	}
	if cfg.DefinedNet.NetworkID != "net-123" {
		t.Errorf("DefinedNet.NetworkID = %q, want %q", cfg.DefinedNet.NetworkID, "net-123")
	}
	if cfg.AppendSuffix != "nebula.example.com" {
		t.Errorf("AppendSuffix = %q, want %q", cfg.AppendSuffix, "nebula.example.com")
	}
	if cfg.TrimSuffix != "example.com" {
		t.Errorf("TrimSuffix = %q, want %q", cfg.TrimSuffix, "example.com")
	}
	if cfg.Prune != "network" {
		t.Errorf("Prune = %q, want %q", cfg.Prune, "network")
	}
	if cfg.RequiredSuffix != ".example.com" {
		t.Errorf("RequiredSuffix = %q, want %q", cfg.RequiredSuffix, ".example.com")
	}
	if len(cfg.RequiredTags) != 1 || cfg.RequiredTags[0] != "publish:yes" {
		t.Errorf("RequiredTags = %v, want [publish:yes]", cfg.RequiredTags)
	}
}

func TestLoadConfig_EnvOverridesFile(t *testing.T) {
	path := writeConfigFile(t, validTOML)

	t.Setenv("NEBULA_DNS_CF_API_TOKEN", "env-cf-token")
	t.Setenv("NEBULA_DNS_DN_NETWORK_ID", "env-net-456")
	t.Setenv("NEBULA_DNS_TRIM_SUFFIX", "env.example.com")

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Cloudflare.APIToken != "env-cf-token" {
		t.Errorf("Cloudflare.APIToken = %q, want %q", cfg.Cloudflare.APIToken, "env-cf-token")
	}
	if cfg.DefinedNet.NetworkID != "env-net-456" {
		t.Errorf("DefinedNet.NetworkID = %q, want %q", cfg.DefinedNet.NetworkID, "env-net-456")
	}
	if cfg.TrimSuffix != "env.example.com" {
		t.Errorf("TrimSuffix = %q, want %q (overridden by env)", cfg.TrimSuffix, "env.example.com")
	}
	// Unset fields should retain file values
	if cfg.DefinedNet.APIToken != "dn-token" {
		t.Errorf("DefinedNet.APIToken = %q, want %q (from file)", cfg.DefinedNet.APIToken, "dn-token")
	}
}

func TestLoadConfig_EnvOnlyNoFile(t *testing.T) {
	t.Setenv("NEBULA_DNS_CF_API_TOKEN", "cf-token")
	t.Setenv("NEBULA_DNS_CF_ZONE_NAME", "example.com")
	t.Setenv("NEBULA_DNS_DN_API_TOKEN", "dn-token")
	t.Setenv("NEBULA_DNS_DN_NETWORK_ID", "net-123")

	cfg, err := LoadConfig("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Cloudflare.APIToken != "cf-token" {
		t.Errorf("Cloudflare.APIToken = %q, want %q", cfg.Cloudflare.APIToken, "cf-token")
	}
	if cfg.Cloudflare.ZoneName != "example.com" {
		t.Errorf("Cloudflare.ZoneName = %q, want %q", cfg.Cloudflare.ZoneName, "example.com")
	}
}

func TestLoadConfig_MissingFileErrors(t *testing.T) {
	_, err := LoadConfig("/nonexistent/config.toml")
	if err == nil {
		t.Fatal("expected error for missing config file, got nil")
	}
}

func TestLoadConfig_AppendSuffixDefaultsToZoneName(t *testing.T) {
	toml := `
[cloudflare]
api_token = "cf-token"
zone_name = "example.com"

[definednet]
api_token = "dn-token"
network_id = "net-123"
`
	path := writeConfigFile(t, toml)

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.AppendSuffix != "example.com" {
		t.Errorf("AppendSuffix = %q, want %q (should default to zone name)", cfg.AppendSuffix, "example.com")
	}
}

func TestLoadConfig_ValidationErrors(t *testing.T) {
	tests := []struct {
		name string
		toml string
	}{
		{"missing all", ``},
		{"missing cf token", `
[cloudflare]
zone_name = "example.com"
[definednet]
api_token = "dn-token"
network_id = "net-123"
`},
		{"missing dn network_id", `
[cloudflare]
api_token = "cf-token"
zone_name = "example.com"
[definednet]
api_token = "dn-token"
`},
		{"boolean trim_suffix", `
trim_suffix = "true"
[cloudflare]
api_token = "cf-token"
zone_name = "example.com"
[definednet]
api_token = "dn-token"
network_id = "net-123"
`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := writeConfigFile(t, tt.toml)
			_, err := LoadConfig(path)
			if err == nil {
				t.Fatal("expected validation error, got nil")
			}
		})
	}
}
