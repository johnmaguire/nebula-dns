# nebula-dns

A script to create and delete [Cloudflare](https://cloudflare.com) DNS records pointing to [Defined Networking](https://defined.net) Managed Nebula nodes, and/or to write them to a hosts file for a self-hosted resolver (e.g. Blocky's `hostsFile` or CoreDNS's `hosts` plugin).

## Development

To build the tool, run `make bin`. This will create a `nebula-dns` binary in the root directory.

To run tests, run `make test`.

## Usage

### Config file

Copy `examples/config.toml` to `config.toml` and fill in the values, then run `./nebula-dns --config config.toml`.

### Environment variables

All configuration can be provided via environment variables, making the config file optional. Environment variables take precedence over values in the config file.

| Variable | Description |
|---|---|
| `NEBULA_DNS_HOSTS_FILE` | Path to write records to in hosts(5) format |
| `NEBULA_DNS_CF_API_TOKEN` | Cloudflare API token (`Zone:DNS:Edit` permission) |
| `NEBULA_DNS_CF_ZONE_NAME` | Cloudflare DNS zone (e.g. `example.com`) |
| `NEBULA_DNS_DN_API_TOKEN` | Defined Networking API token (`hosts:list` permission) |
| `NEBULA_DNS_DN_NETWORK_ID` | Defined Networking network ID (required with Cloudflare) |
| `NEBULA_DNS_REQUIRED_TAGS` | Comma-separated list of required host tags |
| `NEBULA_DNS_REQUIRED_SUFFIX` | Only register hosts with this hostname suffix |
| `NEBULA_DNS_TRIM_SUFFIX` | Domain suffix to strip from DN hostname (e.g. `example.com`) |
| `NEBULA_DNS_APPEND_SUFFIX` | Suffix to append to hostname (defaults to zone name; required without Cloudflare) |
| `NEBULA_DNS_PRUNE` | Prune mode: `none` (default), `all`, or `network` |

### Outputs

At least one output is required. Cloudflare is enabled by setting its API
token and zone; the hosts file by setting `hosts_file`. With both, one run
writes the same records to each.

The hosts file is rewritten in full on every run, so it needs no pruning. It
is replaced by rename, so a resolver in a container must mount its directory
rather than the file itself.

### Docker

Docker images are published to `ghcr.io/johnmaguire/dn-cf-dns` on each tagged release. Images are available for `linux/amd64` and `linux/arm64`.

The container runs `nebula-dns` once immediately on startup, then starts a cron job to run it on a schedule. By default the schedule is every 5 minutes. Set the `NEBULA_DNS_SCHEDULE` environment variable to a cron expression to customize this.

If the initial run fails (e.g. due to missing configuration), the container will exit immediately.

Example `compose.yml`:

```yaml
services:
  nebula-dns:
    image: ghcr.io/johnmaguire/dn-cf-dns:latest
    restart: unless-stopped
    environment:
      - NEBULA_DNS_CF_API_TOKEN=your-cloudflare-token
      - NEBULA_DNS_CF_ZONE_NAME=example.com
      - NEBULA_DNS_DN_API_TOKEN=your-dn-token
      - NEBULA_DNS_DN_NETWORK_ID=your-network-id
      - NEBULA_DNS_REQUIRED_TAGS=publish:yes
      - NEBULA_DNS_TRIM_SUFFIX=example.com
      - NEBULA_DNS_PRUNE=network
      - NEBULA_DNS_SCHEDULE=*/5 * * * *
```
