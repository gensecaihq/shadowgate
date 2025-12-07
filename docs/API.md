# ShadowGate Admin API Reference

The Admin API provides endpoints for health checks, status monitoring, and configuration management.

## Configuration

Enable the Admin API by setting `metrics_addr` in the global configuration:

```yaml
global:
  metrics_addr: "127.0.0.1:9090"
```

**Security Note**: The Admin API should only be accessible from trusted networks. Use firewall rules to restrict access.

## Endpoints

### GET /health

Simple health check endpoint for load balancers and monitoring systems.

**Response**

```json
{
  "status": "ok"
}
```

**Status Codes**
- `200 OK` - Service is healthy

**Example**

```bash
curl http://127.0.0.1:9090/health
```

---

### GET /status

Detailed status information including version, uptime, and resource usage.

**Response**

```json
{
  "status": "running",
  "version": "1.0.0",
  "uptime": "2h30m15s",
  "go_version": "go1.21.0",
  "num_cpu": 8,
  "goroutines": 42,
  "memory": {
    "alloc_bytes": 12582912,
    "total_alloc_bytes": 45678901,
    "sys_bytes": 25165824,
    "num_gc": 15
  }
}
```

**Fields**

| Field | Type | Description |
|-------|------|-------------|
| `status` | string | Service status (`running`) |
| `version` | string | ShadowGate version |
| `uptime` | string | Time since start |
| `go_version` | string | Go runtime version |
| `num_cpu` | int | Available CPU cores |
| `goroutines` | int | Active goroutines |
| `memory.alloc_bytes` | uint64 | Current heap allocation |
| `memory.total_alloc_bytes` | uint64 | Total bytes allocated |
| `memory.sys_bytes` | uint64 | Memory from OS |
| `memory.num_gc` | uint32 | GC cycles completed |

**Example**

```bash
curl http://127.0.0.1:9090/status
```

---

### GET /metrics

Request metrics and statistics.

**Response**

```json
{
  "uptime": "2h30m15s",
  "total_requests": 150000,
  "allowed_requests": 125000,
  "denied_requests": 25000,
  "unique_ips": 5000,
  "avg_response_ms": 12.5,
  "requests_per_sec": 15.2,
  "profile_requests": {
    "c2-front": 100000,
    "phishing": 50000
  },
  "decisions": {
    "allow_forward": 125000,
    "deny_decoy": 24000,
    "drop": 500,
    "redirect": 300,
    "tarpit": 200
  },
  "rule_hits": {
    "ip_allow": 125000,
    "ua_blacklist": 15000,
    "geo_deny": 5000,
    "rate_limit": 5000
  }
}
```

**Fields**

| Field | Type | Description |
|-------|------|-------------|
| `uptime` | string | Time since start |
| `total_requests` | int64 | Total requests processed |
| `allowed_requests` | int64 | Requests forwarded to backends |
| `denied_requests` | int64 | Requests served decoys |
| `unique_ips` | int | Unique client IPs seen |
| `avg_response_ms` | float64 | Average response time |
| `requests_per_sec` | float64 | Current request rate |
| `profile_requests` | map | Requests per profile |
| `decisions` | map | Count by decision type |
| `rule_hits` | map | Count by rule type |

**Example**

```bash
curl http://127.0.0.1:9090/metrics
```

---

### GET /metrics/prometheus

Request metrics in Prometheus exposition format. This endpoint is compatible with Prometheus scrapers.

**Response**

```text
# HELP shadowgate_requests_total Total number of requests processed
# TYPE shadowgate_requests_total counter
shadowgate_requests_total 150000

# HELP shadowgate_requests_allowed_total Total number of allowed requests
# TYPE shadowgate_requests_allowed_total counter
shadowgate_requests_allowed_total 125000

# HELP shadowgate_requests_denied_total Total number of denied requests
# TYPE shadowgate_requests_denied_total counter
shadowgate_requests_denied_total 25000

# HELP shadowgate_unique_ips Number of unique client IPs seen
# TYPE shadowgate_unique_ips gauge
shadowgate_unique_ips 5000

# HELP shadowgate_response_time_ms_avg Average response time in milliseconds
# TYPE shadowgate_response_time_ms_avg gauge
shadowgate_response_time_ms_avg 12.500

# HELP shadowgate_requests_per_second Current request rate
# TYPE shadowgate_requests_per_second gauge
shadowgate_requests_per_second 15.200

# HELP shadowgate_profile_requests_total Requests per profile
# TYPE shadowgate_profile_requests_total counter
shadowgate_profile_requests_total{profile="c2-front"} 100000
shadowgate_profile_requests_total{profile="phishing"} 50000

# HELP shadowgate_decisions_total Counts by decision type
# TYPE shadowgate_decisions_total counter
shadowgate_decisions_total{decision="allow_forward"} 125000
shadowgate_decisions_total{decision="deny_decoy"} 24000

# HELP shadowgate_rule_hits_total Counts by rule type
# TYPE shadowgate_rule_hits_total counter
shadowgate_rule_hits_total{rule="ip_allow"} 125000
shadowgate_rule_hits_total{rule="ua_blacklist"} 15000
```

**Prometheus Configuration**

```yaml
scrape_configs:
  - job_name: 'shadowgate'
    static_configs:
      - targets: ['127.0.0.1:9090']
    metrics_path: '/metrics/prometheus'
```

**Example**

```bash
curl http://127.0.0.1:9090/metrics/prometheus
```

---

### GET /backends

Backend pool status and health information.

**Response**

```json
{
  "profiles": {
    "c2-front": {
      "total": 2,
      "healthy": 2,
      "backends": [
        {
          "name": "c2-primary",
          "url": "http://10.0.1.10:8080",
          "weight": 10,
          "healthy": true,
          "last_check": "2024-01-15T10:30:00Z",
          "last_healthy": "2024-01-15T10:30:00Z",
          "check_count": 1500,
          "fail_count": 0
        },
        {
          "name": "c2-secondary",
          "url": "http://10.0.1.11:8080",
          "weight": 5,
          "healthy": true,
          "last_check": "2024-01-15T10:30:00Z",
          "last_healthy": "2024-01-15T10:30:00Z",
          "check_count": 1500,
          "fail_count": 2
        }
      ]
    }
  }
}
```

**Backend Fields**

| Field | Type | Description |
|-------|------|-------------|
| `name` | string | Backend identifier |
| `url` | string | Backend URL |
| `weight` | int | Load balancing weight |
| `healthy` | bool | Current health status |
| `last_check` | string | Last health check time (RFC3339) |
| `last_healthy` | string | Last successful check time |
| `check_count` | int64 | Total health checks performed |
| `fail_count` | int64 | Failed health checks |

**Example**

```bash
curl http://127.0.0.1:9090/backends
```

---

### POST /reload

Validate configuration file. This endpoint validates the configuration without applying changes.

**Note**: Currently validates configuration only. A service restart is required for changes to take effect. True hot reload may be added in a future version.

**Response (Success)**

```json
{
  "success": true,
  "message": "Configuration validated successfully"
}
```

**Response (Failure)**

```json
{
  "success": false,
  "message": "failed to parse configuration: invalid YAML at line 42"
}
```

**Status Codes**
- `200 OK` - Reload completed (check `success` field)
- `405 Method Not Allowed` - Must use POST method

**Example**

```bash
curl -X POST http://127.0.0.1:9090/reload
```

---

## Error Responses

All endpoints return errors in a consistent format:

```json
{
  "error": "error message"
}
```

**Common Status Codes**

| Code | Description |
|------|-------------|
| `200` | Success |
| `405` | Method not allowed |
| `500` | Internal server error |
| `503` | Service unavailable |

## Monitoring Integration

### JSON Metrics

The `/metrics` endpoint returns JSON format metrics. To integrate with monitoring systems:

```bash
# Fetch metrics as JSON
curl -s http://127.0.0.1:9090/metrics | jq .

# Example: Extract total requests with jq
curl -s http://127.0.0.1:9090/metrics | jq '.total_requests'
```

### Health Check Scripts

```bash
#!/bin/bash
# health_check.sh
response=$(curl -s -o /dev/null -w "%{http_code}" http://127.0.0.1:9090/health)
if [ "$response" = "200" ]; then
    exit 0
else
    exit 1
fi
```

### Alerting Example

```bash
#!/bin/bash
# Check for unhealthy backends
unhealthy=$(curl -s http://127.0.0.1:9090/backends | jq '.profiles[].healthy < .profiles[].total' | grep true)
if [ -n "$unhealthy" ]; then
    echo "Alert: Some backends are unhealthy"
    exit 1
fi
```
