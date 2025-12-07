# ShadowGate Operations Runbook

Operational procedures for deploying, managing, and troubleshooting ShadowGate.

## Table of Contents

1. [Deployment](#deployment)
2. [Configuration Management](#configuration-management)
3. [Monitoring](#monitoring)
4. [Graceful Shutdown](#graceful-shutdown)
5. [Circuit Breaker](#circuit-breaker)
6. [Request Tracing](#request-tracing)
7. [Troubleshooting](#troubleshooting)
8. [Maintenance](#maintenance)

---

## Deployment

### Prerequisites

- Linux server (RHEL 8+, Ubuntu 20.04+, Debian 11+)
- Go 1.21+ (for building from source)
- Network access to backend servers
- TLS certificates (for HTTPS listeners)
- MaxMind GeoIP database (optional)

### Installation Methods

#### Binary Installation

```bash
# Download or build binary
make build

# Create system user
sudo useradd -r -s /sbin/nologin shadowgate

# Create directories
sudo mkdir -p /opt/shadowgate /etc/shadowgate /var/log/shadowgate
sudo chown shadowgate:shadowgate /etc/shadowgate /var/log/shadowgate

# Install binary
sudo cp bin/shadowgate /opt/shadowgate/
sudo chmod 755 /opt/shadowgate/shadowgate

# Install systemd service
sudo cp deploy/systemd/shadowgate.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable shadowgate
```

#### Docker Installation

```bash
# Build image
make docker

# Run container
docker run -d \
  --name shadowgate \
  --restart=always \
  -p 443:8443 \
  -p 9090:9090 \
  -v /etc/shadowgate:/etc/shadowgate:ro \
  -v /var/log/shadowgate:/var/log/shadowgate \
  shadowgate:latest
```

#### Ansible Deployment

```bash
cd deploy/ansible
cp inventory.example inventory
# Edit inventory with your servers
ansible-playbook -i inventory playbook.yml
```

### Post-Installation Verification

```bash
# Check service status
sudo systemctl status shadowgate

# Verify listening ports
ss -tlnp | grep shadowgate

# Test health endpoint (no auth required)
curl http://127.0.0.1:9090/health

# Test authenticated endpoint (if auth configured)
curl -H "Authorization: Bearer your-token" http://127.0.0.1:9090/status

# Check logs
sudo journalctl -u shadowgate -f
```

### Admin API Security

Always configure authentication for production:

```yaml
global:
  admin_api:
    token: "your-secret-token"      # Required for API access
    allowed_ips:                     # Optional IP allowlist
      - "127.0.0.1"
      - "10.0.0.0/8"
```

**Security Notes**:
- `/health` endpoint is always accessible (for load balancer checks)
- All other endpoints require authentication when configured
- IP allowlist is checked before token validation
- Use both for defense in depth

---

## Configuration Management

### Configuration File Location

- Default: `/etc/shadowgate/config.yaml`
- Override with `-config` flag

### Validating Configuration

Always validate before applying:

```bash
/opt/shadowgate/shadowgate -validate -config /etc/shadowgate/config.yaml
```

### Configuration Validation

Validate configuration without restart (changes require restart to take effect):

```bash
# Via SIGHUP (validates config, logs result)
sudo kill -HUP $(pidof shadowgate)

# Via Admin API (validates config, returns result)
curl -X POST http://127.0.0.1:9090/reload

# Apply changes (requires restart)
sudo systemctl restart shadowgate
```

> **Note**: SIGHUP and the `/reload` endpoint validate the configuration but do not apply changes. A full service restart is required for configuration changes to take effect.

### Configuration Backup

```bash
# Before changes
cp /etc/shadowgate/config.yaml /etc/shadowgate/config.yaml.bak.$(date +%Y%m%d)

# Automated backup script
#!/bin/bash
BACKUP_DIR="/var/backups/shadowgate"
mkdir -p "$BACKUP_DIR"
cp /etc/shadowgate/config.yaml "$BACKUP_DIR/config.yaml.$(date +%Y%m%d_%H%M%S)"
find "$BACKUP_DIR" -mtime +30 -delete
```

### TLS Certificate Renewal

```bash
# Update certificates
sudo cp new_cert.crt /etc/shadowgate/server.crt
sudo cp new_key.key /etc/shadowgate/server.key
sudo chown shadowgate:shadowgate /etc/shadowgate/server.*
sudo chmod 600 /etc/shadowgate/server.key

# Restart to load new certificates
sudo systemctl restart shadowgate
```

---

## Monitoring

### Key Metrics to Monitor

| Metric | Warning | Critical | Description |
|--------|---------|----------|-------------|
| `requests_per_sec` | >1000 | >5000 | Request rate spike |
| `avg_response_ms` | >100 | >500 | Latency increase |
| `denied_requests` | >50% | >80% | High block rate |
| `goroutines` | >1000 | >5000 | Goroutine leak |
| `memory.alloc_bytes` | >500MB | >1GB | Memory pressure |
| `backend_error_rate` | >1% | >5% | Backend errors |
| `circuit_breaker_state` | =1 | N/A | Circuit open |
| `backend_healthy` | =0 | N/A | Backend down |
| `backend_latency_ms_avg` | >200 | >1000 | Backend slow |

### Health Check Commands

```bash
# Service health
curl -s http://127.0.0.1:9090/health | jq .

# Backend health and circuit breaker status
curl -s http://127.0.0.1:9090/backends | jq '.profiles[].backends[] | {name, healthy, circuit_breaker: .circuit_breaker.state}'

# Request metrics
curl -s http://127.0.0.1:9090/metrics | jq '{rate: .requests_per_sec, latency: .avg_response_ms}'

# Backend latency metrics
curl -s http://127.0.0.1:9090/metrics | jq '.backend_stats | to_entries[] | {backend: .key, avg_latency: .value.avg_latency_ms, error_rate: .value.error_rate}'

# Circuit breaker status
curl -s http://127.0.0.1:9090/backends | jq '.profiles[].backends[] | select(.circuit_breaker.state != "closed") | {name, state: .circuit_breaker.state}'
```

### Log Analysis

```bash
# Recent errors
journalctl -u shadowgate --since "1 hour ago" | grep -i error

# Top blocked IPs
cat /var/log/shadowgate/access.log | \
  jq -r 'select(.action == "deny_decoy") | .client_ip' | \
  sort | uniq -c | sort -rn | head -20

# Request volume by profile
cat /var/log/shadowgate/access.log | \
  jq -r '.profile_id' | sort | uniq -c

# Top User-Agents blocked
cat /var/log/shadowgate/access.log | \
  jq -r 'select(.action == "deny_decoy") | .user_agent' | \
  sort | uniq -c | sort -rn | head -20
```

### Alerting Rules

```bash
#!/bin/bash
# alert_check.sh - Run via cron every minute

METRICS=$(curl -s http://127.0.0.1:9090/metrics)

# Check request rate
RATE=$(echo "$METRICS" | jq '.requests_per_sec')
if (( $(echo "$RATE > 5000" | bc -l) )); then
    echo "CRITICAL: Request rate $RATE exceeds threshold"
fi

# Check latency
LATENCY=$(echo "$METRICS" | jq '.avg_response_ms')
if (( $(echo "$LATENCY > 500" | bc -l) )); then
    echo "CRITICAL: Latency $LATENCY ms exceeds threshold"
fi

# Check backend health
BACKENDS=$(curl -s http://127.0.0.1:9090/backends)
UNHEALTHY=$(echo "$BACKENDS" | jq '[.profiles[].backends[] | select(.healthy == false)] | length')
if [ "$UNHEALTHY" -gt 0 ]; then
    echo "WARNING: $UNHEALTHY backends unhealthy"
fi
```

---

## Graceful Shutdown

ShadowGate supports graceful shutdown with connection draining.

### How It Works

1. When SIGTERM or SIGINT is received, ShadowGate stops accepting new connections
2. Active connections are allowed to complete within the configured timeout
3. Health checkers stop marking backends as unhealthy
4. Admin API is stopped
5. Once all connections drain (or timeout expires), the process exits

### Configuration

```yaml
global:
  shutdown_timeout: 30  # seconds to wait for connections to drain
```

### Triggering Graceful Shutdown

```bash
# Systemd (recommended)
sudo systemctl stop shadowgate

# Direct signal
kill -SIGTERM $(pidof shadowgate)

# Or
kill -SIGINT $(pidof shadowgate)
```

### Monitoring Shutdown

```bash
# Watch logs during shutdown
journalctl -u shadowgate -f
```

Expected log sequence:
```
Shutting down - draining connections
Health checkers stopped
Admin API stopped
Draining connections (timeout: 30s)
All connections drained successfully
Shutdown complete
```

### Forced Shutdown

If graceful shutdown takes too long:
```bash
kill -SIGKILL $(pidof shadowgate)
```
**Warning**: This drops active connections immediately.

---

## Circuit Breaker

The circuit breaker pattern prevents cascading failures when backends are unhealthy.

### States

| State | Description | Behavior |
|-------|-------------|----------|
| **Closed** | Normal operation | Requests flow to backend |
| **Open** | Backend failing | Requests return 503 immediately |
| **Half-Open** | Testing recovery | Limited requests sent to backend |

### Configuration

Circuit breaker uses sensible defaults:
- **Failure Threshold**: 5 consecutive failures to open circuit
- **Success Threshold**: 2 consecutive successes to close circuit
- **Timeout**: 30 seconds before transitioning from open to half-open

### Monitoring Circuit Breakers

```bash
# Check circuit breaker state via API
curl -s http://127.0.0.1:9090/backends | jq '.profiles[].backends[] | {name, circuit_breaker}'

# Via Prometheus metrics
curl -s http://127.0.0.1:9090/metrics/prometheus | grep circuit_breaker
```

### Circuit Breaker Metrics

| Metric | Description |
|--------|-------------|
| `shadowgate_circuit_breaker_state` | 0=closed, 1=open, 2=half-open |
| `shadowgate_circuit_breaker_failures` | Consecutive failure count |
| `shadowgate_circuit_breaker_successes` | Consecutive success count (half-open) |
| `shadowgate_backend_healthy` | Health check status (1=healthy) |

### Alerting on Circuit Breakers

```bash
#!/bin/bash
# Alert when any circuit breaker is open
OPEN_CIRCUITS=$(curl -s http://127.0.0.1:9090/backends | \
  jq '[.profiles[].backends[] | select(.circuit_breaker.state == "open")] | length')

if [ "$OPEN_CIRCUITS" -gt 0 ]; then
    echo "ALERT: $OPEN_CIRCUITS circuit breaker(s) are OPEN"
    # Send alert via your preferred method
fi
```

### Manual Recovery

If a circuit breaker is stuck open after backend recovery:

1. Restart ShadowGate (circuit breakers reset on startup)
2. Or wait for the automatic half-open transition (30s default)

---

## Request Tracing

ShadowGate supports distributed tracing via the X-Request-ID header.

### How It Works

1. If a request includes `X-Request-ID`, ShadowGate preserves it
2. If no `X-Request-ID` is present, ShadowGate generates a unique ID
3. The ID is included in:
   - Response headers to the client
   - Request headers to the backend
   - Access logs

### Log Format

```json
{
  "timestamp": "2024-01-15T10:30:00Z",
  "request_id": "a1b2c3d4e5f6g7h8",
  "profile_id": "c2-front",
  "client_ip": "10.0.0.1",
  "method": "GET",
  "path": "/api/data",
  "action": "allow_forward",
  "duration_ms": 12.5
}
```

### Correlating Requests

To trace a request across systems:

```bash
# Find request in ShadowGate logs
cat /var/log/shadowgate/access.log | jq 'select(.request_id == "a1b2c3d4e5f6g7h8")'

# Or with journald
journalctl -u shadowgate | grep "a1b2c3d4e5f6g7h8"
```

### Client-Side Tracing

Include your own request ID for end-to-end tracing:

```bash
curl -H "X-Request-ID: my-trace-123" https://your-server/api/endpoint
```

The same ID will be logged by ShadowGate and forwarded to backends.

### Debugging with Request IDs

When investigating issues:

1. Get the request ID from client response headers
2. Search ShadowGate logs for that ID
3. Search backend logs for the same ID

```bash
# Response header shows request ID
curl -v https://your-server/api/endpoint 2>&1 | grep X-Request-ID
```

---

## Troubleshooting

### Service Won't Start

**Check configuration syntax:**
```bash
/opt/shadowgate/shadowgate -validate -config /etc/shadowgate/config.yaml
```

**Check file permissions:**
```bash
ls -la /etc/shadowgate/
# Ensure shadowgate user can read config
# Ensure key files are readable
```

**Check port availability:**
```bash
ss -tlnp | grep :443
# If port in use, identify process
lsof -i :443
```

**Check systemd logs:**
```bash
journalctl -u shadowgate -n 50 --no-pager
```

### High Memory Usage

**Check goroutine count:**
```bash
curl -s http://127.0.0.1:9090/status | jq '.goroutines'
```

**Check memory stats:**
```bash
curl -s http://127.0.0.1:9090/status | jq '.memory'
```

**If goroutine leak suspected:**
```bash
# Restart service
sudo systemctl restart shadowgate
# Monitor memory
watch -n 5 'curl -s http://127.0.0.1:9090/status | jq ".memory.alloc_bytes"'
```

### Backend Connection Issues

**Check backend health:**
```bash
curl -s http://127.0.0.1:9090/backends | jq '.profiles[].backends[]'
```

**Test backend directly:**
```bash
curl -v http://10.0.1.10:8080/health
```

**Check network connectivity:**
```bash
nc -zv 10.0.1.10 8080
```

### Traffic Not Being Forwarded

**Check rule evaluation:**
```bash
# Look for deny decisions in logs
tail -f /var/log/shadowgate/access.log | jq 'select(.action != "allow_forward")'
```

**Verify client IP detection:**
```bash
# Check if X-Forwarded-For is being used correctly
tail -f /var/log/shadowgate/access.log | jq '{ip: .client_ip, action: .action}'
```

**Test with curl:**
```bash
curl -v -H "User-Agent: Mozilla/5.0" https://your-server/test
```

### TLS Errors

**Verify certificate:**
```bash
openssl x509 -in /etc/shadowgate/server.crt -text -noout
```

**Check certificate chain:**
```bash
openssl s_client -connect localhost:443 -servername your-domain.com
```

**Verify key matches cert:**
```bash
openssl x509 -noout -modulus -in server.crt | md5sum
openssl rsa -noout -modulus -in server.key | md5sum
# Output should match
```

---

## Maintenance

### Log Rotation

Create `/etc/logrotate.d/shadowgate`:

```
/var/log/shadowgate/*.log {
    daily
    rotate 30
    compress
    delaycompress
    missingok
    notifempty
    create 0640 shadowgate shadowgate
    copytruncate
}
```

> **Note**: Using `copytruncate` avoids needing to restart the service. Alternatively, configure logging to stdout and use journald.

### GeoIP Database Updates

```bash
#!/bin/bash
# update_geoip.sh - Run weekly via cron

GEOIP_DIR="/opt/geoip"
ACCOUNT_ID="your_account_id"
LICENSE_KEY="your_license_key"

cd "$GEOIP_DIR"

# Download new database
curl -o GeoLite2-Country.mmdb.gz \
  "https://download.maxmind.com/app/geoip_download?edition_id=GeoLite2-Country&license_key=${LICENSE_KEY}&suffix=tar.gz"

# Extract and install
gunzip -c GeoLite2-Country.mmdb.gz > GeoLite2-Country.mmdb.new
mv GeoLite2-Country.mmdb.new GeoLite2-Country.mmdb

# Restart shadowgate to load new database
sudo systemctl restart shadowgate
```

### Backup Procedures

```bash
#!/bin/bash
# backup.sh

BACKUP_DIR="/var/backups/shadowgate/$(date +%Y%m%d)"
mkdir -p "$BACKUP_DIR"

# Backup configuration
cp /etc/shadowgate/config.yaml "$BACKUP_DIR/"
cp /etc/shadowgate/*.crt "$BACKUP_DIR/" 2>/dev/null
cp /etc/shadowgate/*.key "$BACKUP_DIR/" 2>/dev/null

# Backup logs (last 7 days)
find /var/log/shadowgate -mtime -7 -name "*.log" -exec cp {} "$BACKUP_DIR/" \;

# Compress
tar -czf "$BACKUP_DIR.tar.gz" -C "$(dirname $BACKUP_DIR)" "$(basename $BACKUP_DIR)"
rm -rf "$BACKUP_DIR"

# Cleanup old backups (keep 30 days)
find /var/backups/shadowgate -name "*.tar.gz" -mtime +30 -delete
```

### Version Upgrades

```bash
# 1. Backup current binary
sudo cp /opt/shadowgate/shadowgate /opt/shadowgate/shadowgate.bak

# 2. Validate new binary
./shadowgate-new -validate -config /etc/shadowgate/config.yaml

# 3. Stop service
sudo systemctl stop shadowgate

# 4. Install new binary
sudo cp shadowgate-new /opt/shadowgate/shadowgate
sudo chmod 755 /opt/shadowgate/shadowgate

# 5. Start service
sudo systemctl start shadowgate

# 6. Verify
curl http://127.0.0.1:9090/status | jq '.version'

# 7. Rollback if needed
# sudo cp /opt/shadowgate/shadowgate.bak /opt/shadowgate/shadowgate
# sudo systemctl restart shadowgate
```
