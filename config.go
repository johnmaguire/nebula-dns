package main

import (
	"fmt"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/johnmaguire/nebula-dns/internal/envconfig"
)

type AppConfig struct {
	// RequiredTags is the list of tags that must be present on a DN host in
	// the network in order for a DNS record to be created for it.
	RequiredTags []string `toml:"required_tags" envconfig:"NEBULA_DNS_REQUIRED_TAGS"`
	// RequiredSuffix is the suffix that must be present in the DN hostname in
	// order for a DNS record to be created for it.
	RequiredSuffix string `toml:"required_suffix" envconfig:"NEBULA_DNS_REQUIRED_SUFFIX"`

	// TrimSuffix is a domain suffix to strip from the DN hostname before
	// creating the DNS record (e.g. "example.com" turns host.example.com
	// into host, but leaves host.internal untouched). A leading dot is
	// optional. Empty disables trimming.
	TrimSuffix string `toml:"trim_suffix" envconfig:"NEBULA_DNS_TRIM_SUFFIX"`
	// AppendSuffix is the suffix to append to the hostname from DN before
	// creating the DNS record (occurs after TrimSuffix.)
	AppendSuffix string `toml:"append_suffix" envconfig:"NEBULA_DNS_APPEND_SUFFIX"`

	// Prune controls whether and how stale Cloudflare DNS records are deleted.
	// Valid values: "none" (default), "all", "network".
	// "all" deletes any record under AppendSuffix without a matching DN host.
	// "network" only deletes A records whose IP falls within the DN network CIDR.
	Prune string `toml:"prune" envconfig:"NEBULA_DNS_PRUNE"`

	// HostsFile is a path to write the records to in hosts(5) format, for a
	// resolver that serves names from a local file (e.g. Blocky's
	// hostsFile or CoreDNS's hosts plugin). The file always holds exactly the current eligible hosts,
	// so it needs no pruning. Empty disables it.
	HostsFile string `toml:"hosts_file" envconfig:"NEBULA_DNS_HOSTS_FILE"`

	// Cloudflare is the configuration for the Cloudflare API. Leaving both
	// fields empty disables Cloudflare.
	Cloudflare CloudflareConfig `toml:"cloudflare"`

	// Defined is the configuration for the Defined Networking API.
	DefinedNet DefinedConfig `toml:"definednet"`
}

type CloudflareConfig struct {
	APIToken string `toml:"api_token" envconfig:"NEBULA_DNS_CF_API_TOKEN"`
	// ZoneName is the name of the Cloudflare zone to manage DNS records in.
	ZoneName string `toml:"zone_name" envconfig:"NEBULA_DNS_CF_ZONE_NAME"`
}

func (c CloudflareConfig) enabled() bool {
	return c.APIToken != "" || c.ZoneName != ""
}

type DefinedConfig struct {
	APIToken string `toml:"api_token" envconfig:"NEBULA_DNS_DN_API_TOKEN"`
	// NetworkID is the ID of the network in Defined Networking whose CIDRs
	// bound "network" pruning. Required with Cloudflare.
	NetworkID string `toml:"network_id" envconfig:"NEBULA_DNS_DN_NETWORK_ID"`
}

func LoadConfig(path string) (*AppConfig, error) {
	// Load config from file if it exists
	config, err := newConfigFromFile(path)
	if err != nil {
		return nil, err
	}

	// Overlay environment variables onto config
	if err := envconfig.Process(config); err != nil {
		return nil, err
	}

	// Default AppendSuffix to the zone name
	if config.AppendSuffix == "" {
		config.AppendSuffix = config.Cloudflare.ZoneName
	}

	// Validate required fields
	if err := config.validate(); err != nil {
		return nil, err
	}

	return config, nil
}

func (c *AppConfig) validate() error {
	if !c.Cloudflare.enabled() && c.HostsFile == "" {
		return fmt.Errorf("no output configured: set hosts_file and/or the cloudflare section")
	}

	var missing []string
	if c.DefinedNet.APIToken == "" {
		missing = append(missing, "definednet.api_token")
	}
	if c.Cloudflare.enabled() {
		if c.Cloudflare.APIToken == "" {
			missing = append(missing, "cloudflare.api_token")
		}
		if c.Cloudflare.ZoneName == "" {
			missing = append(missing, "cloudflare.zone_name")
		}
		if c.DefinedNet.NetworkID == "" {
			missing = append(missing, "definednet.network_id")
		}
	}
	// Defaults to the zone name, so only a hosts-file-only config can lack it.
	if c.AppendSuffix == "" {
		missing = append(missing, "append_suffix")
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing required config fields: %s", strings.Join(missing, ", "))
	}
	switch strings.ToLower(c.TrimSuffix) {
	case "true", "false":
		return fmt.Errorf("trim_suffix must be a domain suffix to strip (e.g. \"example.com\"), got %q", c.TrimSuffix)
	}
	switch c.Prune {
	case "", "none", "all", "network":
	default:
		return fmt.Errorf("invalid prune value %q: must be \"none\", \"all\", or \"network\"", c.Prune)
	}
	return nil
}

func newConfigFromFile(path string) (*AppConfig, error) {
	var config AppConfig
	if path == "" {
		return &config, nil
	}
	if _, err := toml.DecodeFile(path, &config); err != nil {
		return nil, err
	}
	return &config, nil
}
