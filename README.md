# Gateway Route Manager

Route management service that monitors remote network gateways via HTTP health checks, and dynamically updates the system's routing table using ECMP (Equal-Cost Multi-Path) routing. This enables load balancing of network traffic across multiple healthy gateways.

## Overview

Gateway Route Manager continuously monitors a range of IP addresses via HTTP health checks and automatically manages network routes to include only healthy gateways. By default, it manages the default route (0.0.0.0/0), but can be configured to manage routes with any destination. When gateways become healthy or unhealthy, the routing table is updated in real-time to ensure traffic only flows through working paths.

This tool was specifically designed to work with [Gluetun](https://github.com/qdm12/gluetun), allowing devices on a network to automatically load balance traffic between multiple VPN tunnel exit nodes. See the [gluentun health check documentation here](https://github.com/qdm12/gluetun-wiki/blob/5809ed3a8d5229eeeb5dab574d04665be0fd9348/faq/healthcheck.md#docker-healthcheck).

## Key Features

* Gateway health monitoring via HTTP status checks. A `2xx` response marks the gateway as available, and all other responses (or lack thereof) mark the gateway as inactive.
* Routing table updates via route replacements. Routes are only deleted if no gateways are available, so traffic is not dropped upon routing table update.
* Optional DDNS updates. DNS records for a domain are automatically updated to resolve to all (and only) active gateways. [DynuDNS](https://www.dynu.com/) is currently supported (file an issue for additional providers).
* A Prometheus metrics endpont is available to report information about the gateway and routing table state. See [here for a detailed description of available metrics](./Metrics.md).

## Quick Start

### Command Line Usage

```shell
# Monitor gateways in the range 192.168.1.10-192.168.1.20
gateway-route-manager \
  -start-ip 192.168.1.10 \
  -end-ip 192.168.1.20 \
  -port 9999 \
  -check-period 10s \
  -timeout 5s \
  -log-level debug
```

### Docker Usage

```shell
docker run --rm --name gateway-route-manager \
  --net=host --cap-add=NET_ADMIN \
  gateway-route-manager:<TAG-SET-ME> \
  -start-ip 192.168.1.10 \
  -end-ip 192.168.1.20 \
  -port 8080
```

#### Available Tags

* `<version>` - Minimal distroless image (~12MB) containing only the binary
* `<version>-extended` - Alpine-based image with debugging tools additional tools, based on [netshoot](https://github.com/nicolaka/netshoot). This includes:
  * A full shell
  * Standard CLI tools
  * keepalived, conntrack-tools, nmap, ipvsadm
* `latest`, `latest-extended` - Same as above, but always points to the latest release


## Configuration

### Command Line Flags

| Flag                          | Default      | Description                                                                                      |
| ----------------------------- | ------------ | ------------------------------------------------------------------------------------------------ |
| `-start-ip`                   | *(required)* | Starting IP address for the gateway range                                                        |
| `-end-ip`                     | *(required)* | Ending IP address for the gateway range                                                          |
| `-port`                       | `9999`       | Port to target for health checks                                                                 |
| `-path`                       | `/`          | URL path for health checks                                                                       |
| `-scheme`                     | `http`       | Scheme to use (`http` or `https`)                                                                |
| `-timeout`                    | `1s`         | Timeout for individual health checks                                                             |
| `-check-period`               | `3s`         | How often to perform health checks                                                               |
| `-route`                      | `0.0.0.0/0`  | Route to manage in CIDR notation or 'default' (e.g., `192.168.0.0/16` or `default`)              |
| `-metrics-port`               | `9090`       | Port for Prometheus metrics endpoint                                                             |
| `-log-level`                  | `info`       | Log level (`debug`, `info`, `warn`, `error`)                                                     |
| `-exclude-cidr`               | *(none)*     | Destinations that should not be routed via the gateways (can be specified multiple times)        |
| `-exclude-reserved-cidrs`     | `true`       | Automatically exclude reserved IPv4 destinations (private networks, loopback, multicast, etc.)   |
| `-ddns-provider`              | *(none)*     | DDNS provider to use for updating DNS records (valid values: `dynudns`)                          |
| `-ddns-username`              | *(none)*     | DDNS username (not currently used by any providers)                                              |
| `-ddns-password`              | *(none)*     | DDNS password or API key (required if DDNS provider is specified, falls back to `DDNS_PASSWORD`) |
| `-ddns-hostname`              | *(none)*     | DDNS hostname to update (required if DDNS provider is specified)                                 |
| `-ddns-timeout`               | 60s          | Timeout for DDNS updates                                                                         |
| `-ddns-record-ttl`            | 60s          | TTL to use for new DNS records                                                                   |
| `-ddns-require-ip-address`    | *(none)*     | IPv4 address that must be assigned to an interface for DDNS updates                              |
| `-public-ip-service-hostname` | *(none)*     | Hostname for public IP service (if unset, queries each gateway individually)                     |
| `-public-ip-service-port`     | `443`        | Port for gateway public IP service to fetch public IP addresses                                  |
| `-public-ip-service-scheme`   | `https`      | Scheme for public IP service (`http` or `https`)                                                 |
| `-public-ip-service-path`     | `/`          | URL path for public IP service endpoint                                                          |
| `-public-ip-service-username` | *(none)*     | Username for public IP service HTTP basic authentication                                         |
| `-public-ip-service-password` | *(none)*     | Password for public IP service HTTP basic auth (falls back to `PUBLIC_IP_SERVICE_PASSWORD`)      |

### Example Configurations

#### Basic Setup
```shell
gateway-route-manager -start-ip 10.0.0.10 -end-ip 10.0.0.15
```

#### HTTPS Health Checks
```shell
gateway-route-manager \
  -start-ip 192.168.1.1 \
  -end-ip 192.168.1.5 \
  -scheme https \
  -port 443 \
  -path /health
```

#### Single Gateway
```shell
gateway-route-manager -start-ip 192.168.1.1 -end-ip 192.168.1.1
```

#### Managing Specific Network Routes

To manage a specific network route instead of the default route:

```shell
gateway-route-manager \
  -start-ip 192.168.1.10 \
  -end-ip 192.168.1.15 \
  -route 10.0.0.0/8
```

To manage multiple network routes instead of the default route:

```shell
gateway-route-manager \
  -start-ip 192.168.1.10 \
  -end-ip 192.168.1.15 \
  -route 1.0.0.0/8 \
  -route 2.0.0.0/8
```

#### Excluding Additional Networks

Since reserved networks are excluded by default, you typically only need to add custom exclusions:

```shell
gateway-route-manager \
  -start-ip 192.168.1.10 \
  -end-ip 192.168.1.15 \
  -exclude-cidr 1.2.3.4/32 \
  -exclude-cidr 5.6.7.8/32
```

#### With DDNS Support

Enable Dynamic DNS updates to automatically update DNS records with active gateway public IPs:

**DynuDNS Provider (API key authentication):**
```shell
gateway-route-manager \
  -start-ip 192.168.1.10 \
  -end-ip 192.168.1.15 \
  -ddns-provider dynudns \
  -ddns-password your-api-key \
  -ddns-hostname mygateways.example.com \
  -public-ip-service-port 8000 \
  -public-ip-service-path /v1/ip \
  -ddns-require-ip-address 192.168.1.100 \  # Optional, set if using multiple router instances with VRRP
  -ddns-record-ttl 120s  # Optional
```

Or using environment variable for the API key:

```shell
export DDNS_PASSWORD=your-api-key

gateway-route-manager \
  -start-ip 192.168.1.10 \
  -end-ip 192.168.1.15 \
  -ddns-provider dynudns \
  -ddns-hostname mygateways.example.com
```

#### Disabling Reserved Network Exclusions

To route traffic to reserved networks through gateways:

```shell
gateway-route-manager \
  -start-ip 192.168.1.10 \
  -end-ip 192.168.1.15 \
  -exclude-reserved-cidrs=false
```

### Network Exclusion

Gateway Route Manager provides two ways to exclude network destinations from being routed through the managed gateways:

#### Manual Exclusion with `-exclude-cidr`

The `-exclude-cidr` flag allows you to manually specify network ranges that should **not** be routed through the managed gateways. Traffic to these destinations will continue to use the system's normal routing tables instead of being sent via the gateways.

#### Automatic Exclusion with `-exclude-reserved-cidrs`

The `-exclude-reserved-cidrs` flag (enabled by default) automatically excludes reserved IPv4 address ranges from gateway routing. This includes:

* **Private networks**: `10.0.0.0/8`, `172.16.0.0/12`, `192.168.0.0/16`
* **Loopback**: `127.0.0.0/8`
* **Link-local**: `169.254.0.0/16`
* **Multicast**: `224.0.0.0/3`
* **Test networks**: `192.0.2.0/24`, `198.51.100.0/24`, `203.0.113.0/24`
* **Other reserved ranges**: See [RFC 5735](https://tools.ietf.org/html/rfc5735) for a complete list

This automatic exclusion is useful for:
* Keeping local network traffic on the local network
* Excluding specific networks from VPN routing (VPN split tunneling)
* Ensuring system services (like DNS on 127.0.0.1) continue to work
* Preventing routing loops and connectivity issues

To disable automatic exclusion of reserved networks:
```shell
gateway-route-manager \
  -start-ip 192.168.1.10 \
  -end-ip 192.168.1.15 \
  -exclude-reserved-cidrs=false
```

#### Combining Both Methods

Both exclusion methods can be used together. For example, to exclude reserved networks plus additional custom ranges:

```shell
gateway-route-manager \
  -start-ip 10.0.1.10 \
  -end-ip 10.0.1.15 \
  -exclude-reserved-cidrs=true \
  -exclude-cidr 1.2.3.4/32 \
  -exclude-cidr 5.6.7.8/32
```

#### Legacy Manual Exclusion Example

For systems where you want manual control over exclusions, you can disable automatic exclusion and manually specify networks:

```shell
# Manually exclude all private network ranges (same as automatic, but explicit)
gateway-route-manager \
  -start-ip 10.0.1.10 \
  -end-ip 10.0.1.15 \
  -exclude-reserved-cidrs=false \
  -exclude-cidr 10.0.0.0/8 \
  -exclude-cidr 172.16.0.0/12 \
  -exclude-cidr 192.168.0.0/16
```

### Dynamic DNS (DDNS) Support

Gateway Route Manager can automatically update DNS records with the public IP addresses of active gateways. This is useful for:

* External services that need to connect through your gateways, as the gateway's public IP addresses may frequently change
* Load balancing external traffic across multiple gateway exit points
* Automatic failover when gateways become unavailable

#### How DDNS Works

For each healthy and active gateway, the tool queries `<user>@<pass><scheme>://<hostname>:<port><path>` to get the gateway's public IP address. These variables are defined
via the `-public-ip-service-*` variables. If a hostname is not provided, it defaults to the gateway's internal IP address, allowing each gateway to self-report its public
IP address. This has been designed to integrate with Gluetun's control server.

The response body can either be:
* The IP address in plain text (e.g. `1.2.3.4`)
* A JSON response with one of the following keys containing the IP address in plain text:
  * `public_ip`
  * `ip_address`
  * `ip_addr`
  * `ip`
This should support most common "what's my public IP" providers.

The list of public IP addresses for the active gateways is then used to update a single DNS record (with multiple values) via provider-specific logic (e.g. API calls).

#### Supported DDNS Providers

##### DynuDNS

DynuDNS is a free Dynamic DNS service that supports multiple IP addresses per hostname and configurable TTL values.

**Configuration:**
```shell
gateway-route-manager \
  -start-ip 192.168.1.10 \
  -end-ip 192.168.1.15 \
  -ddns-provider dynudns \
  -ddns-password your-api-key \
  -ddns-hostname your-hostname.dynu.net
```

#### Gateway Public IP Service Requirements

The tool needs to be configured to query another service to get each gateway's public IP address. If no hostname is provided, each active gateway's health check address
will be queried, allowing gateways to self-report their public IP address. The tool will make a HTTP GET request to `<user>@<pass><scheme>://<hostname>:<port><path>`.
The expected response format is one of:

```text
1.2.3.4
```

```json
{"public_ip": "1.2.3.4"}
```

```json
{"ip_address": "1.2.3.4"}
```

```json
{"ip_addr": "1.2.3.4"}
```

```json
{"ip": "1.2.3.4"}
```

## Use Cases

### HA VPN Load Balancing with Gluetun

TODO add link to infra-mk3 after I deploy this to my cluster

### High Availability Internet Gateways

Load balance between multiple internet connections:

```shell
gateway-route-manager \
  -start-ip 192.168.1.1 \
  -end-ip 192.168.1.3 \
  -path /status \
  -check-period 30s \
  -timeout 10s
```

## Requirements

### System Requirements

* Linux with `netlink` support
* `CAP_NET_ADMIN` capability or root privileges for route manipulation
* On some systems `CAP_NET_ADMIN` is not sufficient for netlink modifications, and UID 0 (root) is required as well

## Development

### Building

```shell
# Build binary
make build

# Run tests
make test

# Format code
make fmt

# Check for issues
make vet
```

### Testing

The project includes both unit tests and end-to-end tests:

```shell
# Run unit tests
make test

# Run E2E tests (requires root/NET_ADMIN)
make test-e2e
```

## Troubleshooting

### Common Issues

**Permission Denied**
```
Error: failed to update routes: operation not permitted
```
Solution: Run with `sudo` or ensure `CAP_NET_ADMIN` capability.

**No Route Changes**
```
Gateway check complete: 0/5 gateways active
```
Solution: Verify gateways are responding on the configured port/path.

**Context Deadline Exceeded**
```
Health check failed for 192.168.1.10: context deadline exceeded
```
Solution: Increase timeout with `-timeout` flag or check network connectivity.

### Debug Mode

Enable debug logging to see detailed health check results:

```shell
gateway-route-manager -start-ip 192.168.1.1 -end-ip 192.168.1.5 -log-level debug
```

### Verify Routes

Check the default route:

```shell
ip route show default
```

Expected output with active gateways:
```
default
    nexthop via 192.168.1.1 dev eth0 weight 1 
    nexthop via 192.168.1.2 dev eth0 weight 1
```

## Contributing

Want a feature or find a bug? File and issue and I'll take a look. PRs are welcome for minor fixes and changes, but please open an issue first for anything larger.

