# Gateway Route Manager

Route management service that monitors remote network gateways via HTTP health checks, and dynamically updates the system's routing table using ECMP (Equal-Cost Multi-Path) routing. This enables load balancing of network traffic across multiple healthy gateways.

## Overview

Gateway Route Manager continuously monitors a range of IP addresses via HTTP health checks and automatically manages the default route to include only healthy gateways. When gateways become healthy or unhealthy, the routing table is updated in real-time to ensure traffic only flows through working paths.

This tool was specifically designed to work with [Gluetun](https://github.com/qdm12/gluetun), allowing devices on a network to automatically load balance traffic between multiple VPN tunnel exit nodes. See the [gluentun health check documentation here](https://github.com/qdm12/gluetun-wiki/blob/5809ed3a8d5229eeeb5dab574d04665be0fd9348/faq/healthcheck.md#docker-healthcheck).

## Key Features

* Gateway health monitoring via HTTP status checks. A `2xx` response marks the gateway as available, and all other responses (or lack thereof) mark the gateway as inactive.
* Routing table updates via route replacements. Routes are only deleted if no gateways are available, so traffic is not dropped upon routing table update.
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
  -verbose
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

| Flag            | Default      | Description                               |
| --------------- | ------------ | ----------------------------------------- |
| `-start-ip`     | *(required)* | Starting IP address for the gateway range |
| `-end-ip`       | *(required)* | Ending IP address for the gateway range   |
| `-port`         | `80`         | Port to target for health checks          |
| `-path`         | `/`          | URL path for health checks                |
| `-scheme`       | `http`       | Scheme to use (`http` or `https`)         |
| `-timeout`      | `1s`         | Timeout for individual health checks      |
| `-check-period` | `3s`         | How often to perform health checks        |
| `-metrics-port` | `9090`       | Port for Prometheus metrics endpoint      |
| `-verbose`      | `false`      | Enable verbose logging                    |

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

Enable verbose logging to see detailed health check results:

```shell
gateway-route-manager -start-ip 192.168.1.1 -end-ip 192.168.1.5 -verbose
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

