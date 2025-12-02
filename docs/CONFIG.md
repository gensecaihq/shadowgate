# ShadowGate Configuration Reference

Complete reference for all configuration options.

## Configuration Structure

```yaml
global:
  log: { ... }
  geoip_db_path: string
  metrics_addr: string

profiles:
  - id: string
    listeners: [ ... ]
    backends: [ ... ]
    rules: { ... }
    decoy: { ... }
    shaping: { ... }
```

## Global Settings

### `global.log`

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `level` | string | `info` | Log level: `debug`, `info`, `warn`, `error` |
| `format` | string | `json` | Output format: `json`, `text` |
| `output` | string | `stdout` | Destination: `stdout`, `stderr`, or file path |

```yaml
global:
  log:
    level: info
    format: json
    output: /var/log/shadowgate/access.log
```

### `global.geoip_db_path`

Path to MaxMind GeoIP2 database file (`.mmdb`). Required for `geo_allow`, `geo_deny`, `asn_allow`, `asn_deny` rules.

```yaml
global:
  geoip_db_path: /opt/geoip/GeoLite2-Country.mmdb
```

### `global.metrics_addr`

Address for the metrics API endpoint.

```yaml
global:
  metrics_addr: "127.0.0.1:9090"
```

## Profiles

Each profile defines an independent traffic handling configuration.

### `profiles[].id`

Unique identifier for the profile. Used in logging and metrics.

### `profiles[].listeners`

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `addr` | string | Yes | Listen address (e.g., `0.0.0.0:443`) |
| `protocol` | string | No | `http` or `https` (default: `http`) |
| `tls.cert_file` | string | No | Path to TLS certificate |
| `tls.key_file` | string | No | Path to TLS private key |

```yaml
listeners:
  - addr: "0.0.0.0:443"
    protocol: https
    tls:
      cert_file: /etc/shadowgate/server.crt
      key_file: /etc/shadowgate/server.key
```

### `profiles[].backends`

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `name` | string | Yes | Backend identifier |
| `url` | string | Yes | Backend URL (e.g., `http://10.0.1.10:8080`) |
| `weight` | int | No | Load balancing weight (default: 1) |

```yaml
backends:
  - name: primary
    url: http://10.0.1.10:8080
    weight: 10
  - name: secondary
    url: http://10.0.1.11:8080
    weight: 5
```

## Rules Configuration

Rules determine whether traffic is forwarded to backends or served a decoy.

### Rule Evaluation Logic

1. If `deny` rules match → serve decoy
2. If `allow` rules exist and don't match → serve decoy
3. Otherwise → forward to backend

### Boolean Logic

Rules can be combined using `and`, `or`, `not`, or used as a single `rule`:

```yaml
rules:
  allow:
    and:
      - type: ip_allow
        cidrs: ["10.0.0.0/8"]
      - type: method_allow
        methods: ["GET", "POST"]
```

```yaml
rules:
  deny:
    or:
      - type: ua_blacklist
        patterns: ["(?i)nmap"]
      - type: path_deny
        paths: ["^/admin"]
```

```yaml
rules:
  allow:
    rule:
      type: ip_allow
      cidrs: ["0.0.0.0/0"]
```

## Rule Types Reference

### IP Rules

**`ip_allow`** / **`ip_deny`**

Filter by client IP address or CIDR range.

| Field | Type | Description |
|-------|------|-------------|
| `cidrs` | []string | List of CIDR ranges or single IPs |

```yaml
- type: ip_allow
  cidrs:
    - "10.0.0.0/8"
    - "192.168.1.100"
    - "2001:db8::/32"
```

### GeoIP Rules

**`geo_allow`** / **`geo_deny`**

Filter by country using MaxMind GeoIP database.

| Field | Type | Description |
|-------|------|-------------|
| `countries` | []string | ISO 3166-1 alpha-2 country codes |

```yaml
- type: geo_allow
  countries:
    - "US"
    - "CA"
    - "GB"
```

### ASN Rules

**`asn_allow`** / **`asn_deny`**

Filter by Autonomous System Number.

| Field | Type | Description |
|-------|------|-------------|
| `asns` | []uint | List of AS numbers |

```yaml
- type: asn_deny
  asns:
    - 14061  # DigitalOcean
    - 16276  # OVH
    - 14618  # AWS
```

### User-Agent Rules

**`ua_whitelist`** / **`ua_blacklist`**

Filter by User-Agent header using regex patterns.

| Field | Type | Description |
|-------|------|-------------|
| `patterns` | []string | Regex patterns to match |

```yaml
- type: ua_blacklist
  patterns:
    - "(?i)nmap"
    - "(?i)nikto"
    - "(?i)sqlmap"
    - "(?i)masscan"
    - "(?i)zgrab"
```

### HTTP Method Rules

**`method_allow`** / **`method_deny`**

Filter by HTTP method.

| Field | Type | Description |
|-------|------|-------------|
| `methods` | []string | HTTP methods (GET, POST, PUT, etc.) |

```yaml
- type: method_allow
  methods:
    - "GET"
    - "POST"
```

### Path Rules

**`path_allow`** / **`path_deny`**

Filter by URL path using regex patterns.

| Field | Type | Description |
|-------|------|-------------|
| `paths` | []string | Regex patterns for paths |

```yaml
- type: path_deny
  paths:
    - "^/admin"
    - "^/debug"
    - "^/\\.git"
    - "\\.php$"
```

### Header Rules

**`header_allow`** / **`header_deny`**

Filter by HTTP header presence and value.

| Field | Type | Description |
|-------|------|-------------|
| `header_name` | string | Header name to check |
| `patterns` | []string | Regex patterns for header value |
| `require_header` | bool | If true, header must be present |

```yaml
- type: header_allow
  header_name: "Authorization"
  patterns:
    - "^Bearer .+"
  require_header: true
```

### TLS Rules

**`tls_version`**

Filter by TLS version.

| Field | Type | Description |
|-------|------|-------------|
| `tls_min_version` | string | Minimum TLS version (`1.2`, `1.3`) |
| `tls_max_version` | string | Maximum TLS version |

```yaml
- type: tls_version
  tls_min_version: "1.2"
```

### SNI Rules

**`sni_allow`** / **`sni_deny`**

Filter by TLS Server Name Indication.

| Field | Type | Description |
|-------|------|-------------|
| `sni_patterns` | []string | Regex patterns for SNI |
| `require_sni` | bool | If true, SNI must be present |

```yaml
- type: sni_allow
  sni_patterns:
    - ".*\\.example\\.com$"
  require_sni: true
```

### Rate Limiting

**`rate_limit`**

Limit requests per source IP.

| Field | Type | Description |
|-------|------|-------------|
| `max_requests` | int | Maximum requests per window |
| `window` | string | Time window (e.g., `1m`, `1h`) |

```yaml
- type: rate_limit
  max_requests: 100
  window: "1m"
```

### Time Window Rules

**`time_window`**

Allow/deny based on time of day.

| Field | Type | Description |
|-------|------|-------------|
| `time_windows` | []TimeWindow | List of allowed time windows |

```yaml
- type: time_window
  time_windows:
    - days: ["mon", "tue", "wed", "thu", "fri"]
      start: "09:00"
      end: "17:00"
```

## Decoy Configuration

Decoy responses are served when traffic is denied.

| Field | Type | Description |
|-------|------|-------------|
| `mode` | string | `static` or `redirect` |
| `status_code` | int | HTTP status code (static mode) |
| `body` | string | Inline response body |
| `body_file` | string | Path to response body file |
| `redirect_to` | string | Redirect URL (redirect mode) |

### Static Decoy

```yaml
decoy:
  mode: static
  status_code: 404
  body: |
    <!DOCTYPE html>
    <html>
    <head><title>404 Not Found</title></head>
    <body><h1>Not Found</h1></body>
    </html>
```

### Redirect Decoy

```yaml
decoy:
  mode: redirect
  redirect_to: "https://www.google.com"
```

### File-Based Decoy

```yaml
decoy:
  mode: static
  status_code: 200
  body_file: /etc/shadowgate/decoy/index.html
```

## Traffic Shaping (Planned)

> **Note**: Traffic shaping configuration is parsed but not yet implemented. Use tarpit decoy mode for delayed responses.

| Field | Type | Description |
|-------|------|-------------|
| `delay_min` | duration | Minimum delay (planned) |
| `delay_max` | duration | Maximum delay (planned) |

## Complete Example

```yaml
global:
  log:
    level: info
    format: json
    output: /var/log/shadowgate/access.log
  geoip_db_path: /opt/geoip/GeoLite2-Country.mmdb
  metrics_addr: "127.0.0.1:9090"

profiles:
  - id: c2-front
    listeners:
      - addr: "0.0.0.0:443"
        protocol: https
        tls:
          cert_file: /etc/shadowgate/server.crt
          key_file: /etc/shadowgate/server.key

    backends:
      - name: c2-primary
        url: http://10.0.1.10:8080
        weight: 10
      - name: c2-secondary
        url: http://10.0.1.11:8080
        weight: 5

    rules:
      deny:
        or:
          - type: ua_blacklist
            patterns:
              - "(?i)nmap"
              - "(?i)nikto"
          - type: path_deny
            paths:
              - "^/admin"
              - "^/\\.git"
          - type: asn_deny
            asns:
              - 14061
              - 16276

      allow:
        and:
          - type: geo_allow
            countries: ["US", "CA"]
          - type: rate_limit
            max_requests: 100
            window: "1m"

    decoy:
      mode: static
      status_code: 404
      body_file: /etc/shadowgate/decoy/404.html
```
