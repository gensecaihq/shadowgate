# ShadowGate

[![Go Version](https://img.shields.io/badge/Go-1.21+-00ADD8?style=flat&logo=go&logoColor=white)](https://go.dev/)
[![License](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![Go Report Card](https://goreportcard.com/badge/github.com/alokemajumder/shadowgate)](https://goreportcard.com/report/github.com/alokemajumder/shadowgate)
[![Tests](https://img.shields.io/badge/Tests-80+-success?style=flat&logo=checkmarx&logoColor=white)](https://github.com/alokemajumder/shadowgate)
[![Docker](https://img.shields.io/badge/Docker-Ready-2496ED?style=flat&logo=docker&logoColor=white)](Dockerfile)
[![Platform](https://img.shields.io/badge/Platform-Linux%20%7C%20macOS%20%7C%20Windows-lightgrey?style=flat)](https://github.com/alokemajumder/shadowgate)

[![Reverse Proxy](https://img.shields.io/badge/Feature-Reverse%20Proxy-orange?style=flat)](docs/CONFIG.md)
[![GeoIP Filtering](https://img.shields.io/badge/Feature-GeoIP%20Filtering-orange?style=flat)](docs/CONFIG.md)
[![Rate Limiting](https://img.shields.io/badge/Feature-Rate%20Limiting-orange?style=flat)](docs/CONFIG.md)
[![TLS Termination](https://img.shields.io/badge/Feature-TLS%20Termination-orange?style=flat)](docs/CONFIG.md)
[![Hot Reload](https://img.shields.io/badge/Feature-Hot%20Reload-orange?style=flat)](docs/CONFIG.md)
[![Admin API](https://img.shields.io/badge/Feature-Admin%20API-orange?style=flat)](docs/API.md)

[![Terraform](https://img.shields.io/badge/IaC-Terraform-7B42BC?style=flat&logo=terraform&logoColor=white)](deploy/terraform/)
[![Ansible](https://img.shields.io/badge/IaC-Ansible-EE0000?style=flat&logo=ansible&logoColor=white)](deploy/ansible/)
[![systemd](https://img.shields.io/badge/Service-systemd-FCC624?style=flat&logo=linux&logoColor=black)](deploy/systemd/)

---

A high-performance stealth redirector and deception gateway written in Go. ShadowGate sits in front of backend servers to filter traffic, serve decoys to unwanted visitors, and protect infrastructure from scanners and automated tools.

## Features

### Traffic Filtering
- **IP/CIDR Rules** - Allow/deny based on IP addresses and subnets (IPv4/IPv6)
- **GeoIP Rules** - Filter by country using MaxMind GeoIP database
- **ASN Rules** - Filter by Autonomous System Number
- **User-Agent Rules** - Regex-based whitelist/blacklist patterns
- **HTTP Rules** - Method, path, and header filtering with regex support
- **TLS Rules** - Filter by TLS version and SNI patterns
- **Rate Limiting** - Per-IP request rate limiting with configurable windows
- **Time Windows** - Allow/deny based on time of day and day of week
- **Boolean Logic** - Combine rules with AND, OR, NOT operators

### Deception & Proxying
- **Reverse Proxy** - HTTP/HTTPS proxying to backend servers
- **Load Balancing** - Round-robin and weighted backend selection with health awareness
- **Health Checks** - Automatic backend health monitoring with per-backend custom endpoints
- **Circuit Breaker** - Automatic backend failover with configurable thresholds
- **Request Retry** - Automatic retry on backend failure with failover to healthy backends
- **Static Decoys** - Serve configurable fake responses to blocked traffic
- **Redirects** - Send 3xx redirects to external sites
- **Tarpit** - Slow responses to waste attacker resources

### Operations
- **Structured Logging** - JSON logging with request metadata and request ID tracing
- **Request Tracing** - X-Request-ID header propagation for distributed tracing
- **Metrics API** - Real-time statistics via REST endpoint (JSON and Prometheus formats)
- **Backend Metrics** - Per-backend latency, error rates, and request counts
- **Admin API** - Health, status, backends, metrics, and config validation endpoints
- **API Authentication** - Bearer token and IP allowlist for admin API security
- **Config Validation** - SIGHUP-based configuration validation
- **Graceful Shutdown** - Connection draining with configurable timeout
- **TLS Termination** - HTTPS listeners with configurable certificates

## Quick Start

### Build

```bash
# Clone repository
git clone https://github.com/alokemajumder/shadowgate.git
cd shadowgate

# Build binary
make build

# Run tests
make test
```

### Run

```bash
# Validate configuration
./bin/shadowgate -validate -config configs/example.yaml

# Run with configuration
./bin/shadowgate -config configs/example.yaml

# Run with version info
./bin/shadowgate -version
```

### Docker

```bash
# Build image
make docker

# Run container
docker run -d \
  --name shadowgate \
  -p 8080:8080 \
  -p 9090:9090 \
  -v /path/to/config.yaml:/etc/shadowgate/config.yaml:ro \
  shadowgate:latest -config /etc/shadowgate/config.yaml
```

## Configuration

Minimal configuration example:

```yaml
global:
  log:
    level: info
    format: json
    output: stdout
  metrics_addr: "127.0.0.1:9090"
  admin_api:
    token: "your-secret-token"        # Optional: Bearer token for API auth
    allowed_ips: ["127.0.0.1"]        # Optional: IP allowlist for API
  shutdown_timeout: 30                 # Graceful shutdown timeout (seconds)

profiles:
  - id: default
    listeners:
      - addr: "0.0.0.0:8080"
        protocol: http

    backends:
      - name: backend1
        url: http://127.0.0.1:9000
        weight: 10
        timeout: 30s                   # Per-backend timeout
        health_check_path: /health     # Custom health endpoint

    rules:
      allow:
        rule:
          type: ip_allow
          cidrs:
            - "10.0.0.0/8"
            - "192.168.0.0/16"

    decoy:
      mode: static
      status_code: 404
      body: "<html><body>Not Found</body></html>"
```

See [docs/CONFIG.md](docs/CONFIG.md) for complete configuration reference.

## Rule Types

| Type | Description |
|------|-------------|
| `ip_allow` / `ip_deny` | Filter by IP address or CIDR range |
| `geo_allow` / `geo_deny` | Filter by country (requires GeoIP database) |
| `asn_allow` / `asn_deny` | Filter by AS number (requires GeoIP database) |
| `ua_whitelist` / `ua_blacklist` | Filter by User-Agent regex patterns |
| `method_allow` / `method_deny` | Filter by HTTP method |
| `path_allow` / `path_deny` | Filter by URL path regex patterns |
| `header_allow` / `header_deny` | Filter by HTTP header presence/value |
| `tls_version` | Filter by minimum/maximum TLS version |
| `sni_allow` / `sni_deny` | Filter by TLS SNI patterns |
| `rate_limit` | Limit requests per source IP |
| `time_window` | Allow during specific time windows |

## Admin API

The Admin API provides endpoints for monitoring and management:

| Endpoint | Method | Auth | Description |
|----------|--------|------|-------------|
| `/health` | GET | No | Health check (returns `{"status": "ok"}`) |
| `/status` | GET | Yes | System status with version, uptime, memory |
| `/metrics` | GET | Yes | Request statistics and counters (JSON) |
| `/metrics/prometheus` | GET | Yes | Prometheus-format metrics |
| `/backends` | GET | Yes | Backend health and circuit breaker status |
| `/reload` | POST | Yes | Trigger configuration validation |

**Authentication**: When configured, endpoints (except `/health`) require:
- Bearer token via `Authorization: Bearer <token>` header
- Request from allowed IP (if IP allowlist configured)

See [docs/API.md](docs/API.md) for complete API reference.

## Documentation

- [Configuration Reference](docs/CONFIG.md) - All configuration options
- [Admin API Reference](docs/API.md) - REST API documentation
- [Operations Runbook](docs/OPERATIONS.md) - Deployment and maintenance

## Project Structure

```
shadowgate/
├── cmd/shadowgate/      # Main application entry point
├── internal/
│   ├── admin/           # Admin API server
│   ├── config/          # Configuration parsing and validation
│   ├── decision/        # Decision engine (allow/deny/redirect)
│   ├── decoy/           # Deception strategies
│   ├── gateway/         # Main HTTP handler
│   ├── geoip/           # MaxMind GeoIP integration
│   ├── honeypot/        # Honeypot path detection
│   ├── listener/        # Network listeners
│   ├── logging/         # Structured logging
│   ├── metrics/         # Metrics collection
│   ├── profile/         # Profile management
│   ├── proxy/           # Backend proxy and load balancing
│   └── rules/           # Rule engine and implementations
├── configs/             # Example configurations
├── deploy/
│   ├── ansible/         # Ansible role and playbook
│   ├── systemd/         # systemd unit file
│   └── terraform/       # Terraform module for AWS
├── docs/                # Documentation
├── Dockerfile
├── Makefile
└── README.md
```

## Example Configurations

| File | Use Case |
|------|----------|
| `configs/minimal.yaml` | Simple reverse proxy with IP filtering |
| `configs/example.yaml` | Basic configuration with common options |
| `configs/c2-front.yaml` | C2 server protection |
| `configs/phishing-front.yaml` | Phishing infrastructure protection |
| `configs/payload-delivery.yaml` | Payload server with strict filtering |
| `configs/advanced.yaml` | All advanced filtering features |

## Development

### Prerequisites

- Go 1.21 or later
- Make

### Building

```bash
# Build for current platform
make build

# Run tests
make test

# Run tests with coverage
make test-cover

# Build Docker image
make docker
```

### Testing

```bash
# Run all tests
go test ./...

# Run with verbose output
go test -v ./...

# Run benchmarks
go test -bench=. ./internal/rules/
go test -bench=. ./internal/proxy/

# Run fuzz tests (2 seconds each)
go test -fuzz=FuzzIPRule -fuzztime=2s ./internal/rules/
```

## Requirements

- **Go 1.21+** for building
- **MaxMind GeoIP database** (optional, for geo/ASN rules)
- **TLS certificates** (for HTTPS listeners)

## Security Considerations

- Run as non-root user in production
- Configure Admin API authentication (bearer token and/or IP allowlist)
- Restrict Admin API to localhost or trusted networks
- Configure `trusted_proxies` when behind load balancers to prevent IP spoofing
- Use TLS for all external-facing listeners
- Set appropriate `max_request_body` to prevent DoS attacks
- Regularly update GeoIP database
- Review logs for suspicious activity
- Use request ID tracing (X-Request-ID) for incident investigation

## License

MIT License - see [LICENSE](LICENSE) for details.

## Contributing

Contributions are welcome. Please:

1. Fork the repository
2. Create a feature branch
3. Add tests for new functionality
4. Ensure all tests pass
5. Submit a pull request

## Disclaimer

This software is provided for legitimate security testing and infrastructure protection purposes. Users are responsible for ensuring compliance with applicable laws and regulations. The authors are not responsible for misuse of this software.
