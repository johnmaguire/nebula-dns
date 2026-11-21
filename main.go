// nebula-dns is a tool for publishing DNS records for hosts that exist in
// Defined Networking, to Cloudflare and/or a hosts file.
//
// This tool is expected to be run as a one-shot job periodically (e.g. once
// a minute) to ensure that DNS records are kept up to date.
//
// It is too bad that Defined Networking does not support webhooks or we would
// be able to trigger this tool on demand when hosts are created or destroyed.
package main

import (
	"context"
	"fmt"
	"net/mail"
	"net/netip"
	"os"
	"runtime/debug"
	"strings"

	"github.com/cloudflare/cloudflare-go"
	"github.com/rs/zerolog/log"
	"github.com/urfave/cli/v3"
)

// version can be set at build time via -ldflags "-X main.version=1.2.3".
// If not set, it falls back to the module version from Go's embedded build info
// (populated by go install module@version).
var version = ""

func init() {
	if version != "" {
		return
	}
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" {
		version = info.Main.Version
	} else {
		version = "dev"
	}
}

func main() {
	if err := mainWithErr(); err != nil {
		log.Fatal().Err(err).Send()
	}
}

func mainWithErr() error {
	cmd := &cli.Command{
		Name:        "nebula-dns",
		Version:     version,
		Description: "nebula-dns manages DNS records in Cloudflare or a hosts file based on Defined Networking hosts",
		Authors: []any{
			&mail.Address{Name: "John Maguire", Address: "contact@johnmaguire.me"},
		},
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "config", Usage: "path to config file"},
		},
		Action: func(ctx context.Context, c *cli.Command) error {
			// Read the config file
			cfg, err := LoadConfig(c.String("config"))
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}

			records, err := collectRecords(cfg)
			if err != nil {
				return err
			}

			if cfg.HostsFile != "" {
				changed, err := WriteHostsFile(cfg.HostsFile, records)
				if err != nil {
					return fmt.Errorf("failed to write hosts file: %w", err)
				}
				log.Info().Str("path", cfg.HostsFile).Bool("changed", changed).
					Int("records", len(records)).Msg("Wrote hosts file")
			}

			if cfg.Cloudflare.enabled() {
				if err := syncCloudflare(cfg, records); err != nil {
					return err
				}
			}

			return nil
		},
	}

	if err := cmd.Run(context.Background(), os.Args); err != nil {
		return err
	}

	return nil
}

// collectRecords fetches the eligible DN hosts and derives the DNS name and
// addresses for each.
func collectRecords(cfg *AppConfig) ([]HostRecord, error) {
	// Filter the DN hosts based on the following criteria:
	// - Presence of a specific tag (e.g. "public-dns:yes")
	// - Hostname contains a specific suffix (e.g. ".example.com")
	log.Info().
		Str("requiredSuffix", cfg.RequiredSuffix).
		Str("requiredTags", strings.Join(cfg.RequiredTags, ",")).
		Msg("Collecting eligible Defined.net Managed Nebula hosts")

	hosts, err := FilterHosts(cfg.DefinedNet.APIToken, func(h Host) bool {
		// Make sure any required suffix is present
		if !strings.HasSuffix(h.Hostname, cfg.RequiredSuffix) {
			return false
		}

		// Make sure all required tags are present
		hostTags := map[string]struct{}{}
		for _, tag := range h.Tags {
			hostTags[tag] = struct{}{}
		}

		for _, tag := range cfg.RequiredTags {
			if _, ok := hostTags[tag]; !ok {
				return false
			}
		}

		return true
	})
	if err != nil {
		return nil, fmt.Errorf("failed to collect eligible hosts: %w", err)
	}

	log.Info().Int("eligibleHosts", len(hosts)).Msgf("Found %d eligible hosts", len(hosts))

	records := make([]HostRecord, 0, len(hosts))
	for _, host := range hosts {
		hostname := host.Hostname
		l := log.Info().Str("initialHostname", hostname)
		if cfg.TrimSuffix != "" {
			hostname = trimSuffix(hostname, cfg.TrimSuffix)
			l = l.Str("trimmedHostname", hostname)
		}
		hostname = strings.ToLower(hostname + "." + cfg.AppendSuffix)

		// Skip rather than fail: one badly named host should not freeze
		// every other host's records.
		if !validHostname(hostname) {
			log.Warn().Str("hostID", host.ID).Str("hostname", hostname).
				Msg("Skipping host whose name is not a valid DNS name")
			continue
		}

		r := HostRecord{Name: hostname}
		for _, ip := range host.IPAddresses {
			addr, err := netip.ParseAddr(ip)
			if err != nil {
				return nil, fmt.Errorf("failed to parse IP %q for %s: %w", ip, hostname, err)
			}
			r.IPs = append(r.IPs, addr)
		}
		l.Str("finalHostname", hostname).Strs("ipAddresses", host.IPAddresses).Msg("Collected host")

		records = append(records, r)
	}

	return records, nil
}

// syncCloudflare upserts an A record per IPv4 address and an AAAA record per
// IPv6 address, then prunes according to cfg.Prune.
func syncCloudflare(cfg *AppConfig, records []HostRecord) error {
	cf, err := cloudflare.NewWithAPIToken(cfg.Cloudflare.APIToken)
	if err != nil {
		return err
	}

	// Find the Cloudflare zone ID for the zone we're interested in
	zoneID, err := GetZoneID(cf, cfg.Cloudflare.ZoneName)
	if err != nil {
		return fmt.Errorf("failed to get zone ID: %w", err)
	}
	log.Info().Str("zoneID", zoneID).Msgf("Found Cloudflare zone ID for %s", cfg.Cloudflare.ZoneName)

	// Get the network CIDRs (IPv4 and, when present, IPv6) for the network we're interested in
	cidrs, err := GetNetworkCIDRs(cfg.DefinedNet.APIToken, cfg.DefinedNet.NetworkID)
	if err != nil {
		return fmt.Errorf("failed to get network CIDRs: %w", err)
	}
	cidrStrs := make([]string, len(cidrs))
	for i, c := range cidrs {
		cidrStrs[i] = c.String()
	}
	log.Info().Strs("networkCIDRs", cidrStrs).Msgf("Found network CIDRs for network %s", cfg.DefinedNet.NetworkID)

	// Track managed records by (hostname, recordType) so pruning can delete
	// stale records (e.g. an AAAA at a name whose host no longer has an IPv6
	// address).
	managed := map[string]map[string]struct{}{}
	for _, r := range records {
		log.Info().Str("hostname", r.Name).Msg("Creating Cloudflare DNS records")

		types := map[string]struct{}{}
		for _, ip := range r.IPs {
			recordType, err := CreateRecord(cf, zoneID, r.Name, ip)
			if err != nil {
				// TODO: Log the error and continue
				return fmt.Errorf("failed to create record for %s -> %s: %w", r.Name, ip, err)
			}
			types[recordType] = struct{}{}
		}

		managed[r.Name] = types
	}

	// For any records within the target zone that do not have a corresponding
	// host in Defined Networking, delete the record
	if cfg.Prune == "all" || cfg.Prune == "network" {
		log.Info().Str("zoneID", zoneID).Str("mode", cfg.Prune).
			Msg("Pruning Cloudflare DNS records")

		err := IterateRecords(cf, zoneID, func(r Record) error {
			if !underSuffix(r.Name, cfg.AppendSuffix) {
				return nil
			}

			// Keep records we manage at this exact (name, type).
			if types, ok := managed[r.Name]; ok {
				if _, ok := types[r.Type]; ok {
					return nil
				}
			}

			// This tool only ever creates A/AAAA records, so never prune other
			// record types (CNAME, TXT, MX, ...) even in "all" mode.
			if r.Type != "A" && r.Type != "AAAA" {
				return nil
			}

			// In network mode, further restrict pruning to records whose IP
			// falls within one of the Nebula network's CIDRs.
			if cfg.Prune == "network" {
				ip, err := netip.ParseAddr(r.Content)
				if err != nil {
					return fmt.Errorf("failed to parse IP address from record content: %w", err)
				}
				inNetwork := false
				for _, c := range cidrs {
					if c.Contains(ip) {
						inNetwork = true
						break
					}
				}
				if !inNetwork {
					return nil
				}
			}

			log.Info().Str("recordID", r.ID).
				Str("recordName", r.Name).
				Str("recordType", r.Type).
				Msg("Pruning stale DNS record")

			if err := DeleteRecord(cf, zoneID, r.ID); err != nil {
				return fmt.Errorf("failed to delete record: %w", err)
			}

			return nil
		})
		if err != nil {
			return fmt.Errorf("error during host prune iteration: %w", err)
		}
	}

	return nil
}

// underSuffix reports whether name is a strict subdomain of suffix. The label
// boundary keeps append_suffix "n.example.com" from claiming xn.example.com,
// and the suffix itself is excluded because records are only ever created
// beneath it (it may be the zone apex).
func underSuffix(name, suffix string) bool {
	return strings.HasSuffix(strings.ToLower(name), "."+strings.ToLower(suffix))
}

// trimSuffix removes suffix (leading dot optional) and its joining dot from
// hostname, comparing case-insensitively. Hostnames that do not end in the
// suffix at a label boundary, or that consist solely of it, are returned
// unchanged.
func trimSuffix(hostname, suffix string) string {
	suffix = "." + strings.TrimPrefix(suffix, ".")
	if len(hostname) > len(suffix) && strings.EqualFold(hostname[len(hostname)-len(suffix):], suffix) {
		return hostname[:len(hostname)-len(suffix)]
	}
	return hostname
}
