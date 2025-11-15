# go-network-proxy

A high-performance HTTP/HTTPS proxy server written in Go with advanced traffic management, monitoring, and security features.

## Overview

go-network-proxy is a flexible forward proxy implementation that supports both HTTP and HTTPS traffic with comprehensive features including domain blocking, rate limiting, traffic analytics, and real-time monitoring through Prometheus and Grafana.

## Features

### Current Implementation

- **HTTP/HTTPS Proxy**: Full support for both HTTP and HTTPS (via CONNECT tunneling)
- **Domain Blocking**: Configurable blocklist with exact and wildcard domain matching
- **Rate Limiting**: 
  - In-memory rate limiting per IP address
  - Distributed Redis-based rate limiting for multi-instance deployments
  - EVALSHA optimization for Redis performance
- **Metrics & Monitoring**:
  - Prometheus metrics integration
  - Grafana dashboards for visualization
  - Request tracking, duration, status codes, and active connections
- **Performance Optimization**:
  - Connection pooling with configurable limits
  - Efficient hop-by-hop header handling
  - Concurrent request handling
- **Security**:
  - TLS/SSL support
  - IP-based access control
  - Request filtering and blocking

### In Development

- Advanced caching mechanisms (in-memory, persisted, and distributed)
- Load balancing across multiple upstream servers
- Traffic analytics and insights
- Zero-trust gateway implementation

## Architecture

```
┌─────────────────────────────────────────────────────────┐
│                    Client Application                    │
└─────────────────────┬───────────────────────────────────┘
                      │
                      ▼
┌─────────────────────────────────────────────────────────┐
│                   HTTP Proxy Server                      │
│  ┌────────────────────────────────────────────────────┐ │
│  │         Rate Limiter (Memory/Redis)                │ │
│  └────────────────────────────────────────────────────┘ │
│  ┌────────────────────────────────────────────────────┐ │
│  │         Blocklist Manager                          │ │
│  └────────────────────────────────────────────────────┘ │
│  ┌────────────────────────────────────────────────────┐ │
│  │    HTTP Handler    │    HTTPS Tunnel Handler      │ │
│  └────────────────────────────────────────────────────┘ │
│  ┌────────────────────────────────────────────────────┐ │
│  │         Prometheus Metrics Collector               │ │
│  └────────────────────────────────────────────────────┘ │
└─────────────────────┬───────────────────────────────────┘
                      │
                      ▼
┌─────────────────────────────────────────────────────────┐
│                  Destination Server                      │
└─────────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────┐
│              Monitoring Stack (Optional)                 │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐  │
│  │  Prometheus  │  │   Grafana    │  │    Redis     │  │
│  └──────────────┘  └──────────────┘  └──────────────┘  │
└─────────────────────────────────────────────────────────┘
```

## Installation

### Prerequisites

- Go 1.24.0 or higher
- Docker and Docker Compose (optional, for monitoring stack)
- Redis (optional, for distributed rate limiting)

### Build from Source

```bash
git clone https://github.com/aluko123/go-network-proxy.git
cd go-network-proxy/http-proxy
go build -o proxy-server main.go
```

## Configuration

### Environment Variables

Copy the example environment file and configure as needed:

```bash
cp .env.example .env
```

Available configuration options:

| Variable | Default | Description |
|----------|---------|-------------|
| `PROMETHEUS_PORT` | 9090 | Prometheus server port |
| `GRAFANA_PORT` | 3000 | Grafana dashboard port |
| `GRAFANA_ADMIN_USER` | admin | Grafana admin username |
| `GRAFANA_ADMIN_PASSWORD` | admin | Grafana admin password |
| `REDIS_PORT` | 6379 | Redis server port |
| `PROXY_PORT` | 8080 | Proxy server port |

### Blocklist Configuration

Edit `http-proxy/blocklist/blocklist.json` to configure blocked domains:

```json
{
  "blocked_domains": [
    "example.com",
    "*.ads.example.com",
    "malicious-site.com"
  ]
}
```

Supports:
- Exact domain matching: `example.com`
- Wildcard patterns: `*.ads.example.com`

## Usage

### Basic HTTP Proxy

```bash
cd http-proxy
./proxy-server -proto http
```

### HTTPS Proxy with TLS

```bash
cd http-proxy
./proxy-server -proto https -pem /path/to/cert.pem -key /path/to/key.key
```

### With In-Memory Rate Limiting

```bash
./proxy-server -proto http -limiter memory -rate-limit 100 -rate-burst 20
```

### With Redis Rate Limiting

```bash
# Start Redis first
docker-compose up -d redis

# Start proxy with Redis limiter
./proxy-server -proto http -limiter redis -redis-addr localhost:6379 -rate-limit 100
```

### Command-Line Options

```
-proto string
    Protocol to use: http or https (default "http")
-pem string
    Path to PEM certificate file (default "server.pem")
-key string
    Path to private key file (default "server.key")
-limiter string
    Rate limiter type: memory or redis (default "memory")
-redis-addr string
    Redis server address (default "localhost:6379")
-rate-limit int
    Requests per minute per IP (default 100)
-rate-burst int
    Burst size for rate limiter (default 20)
-debug
    Enable debug logging
```

## Using the Proxy

### Configure Client Applications

Set the proxy in your client application or system settings:

```bash
# Using curl
curl -x http://localhost:8080 http://example.com

# Using environment variables
export HTTP_PROXY=http://localhost:8080
export HTTPS_PROXY=http://localhost:8080

# Test HTTPS tunneling
curl -x http://localhost:8080 https://www.google.com
```

### Browser Configuration

Configure your browser to use `localhost:8080` as the HTTP/HTTPS proxy.

## Monitoring

### Quick Start

Start the monitoring stack:

```bash
docker-compose up -d
```

This launches:
- Prometheus at http://localhost:9090
- Grafana at http://localhost:3000
- Redis at localhost:6379

### Accessing Metrics

Prometheus metrics are exposed at:
```
http://localhost:8080/metrics
```

Available metrics:
- `proxy_requests_total`: Total number of proxy requests
- `proxy_blocked_requests_total`: Total blocked requests
- `proxy_request_duration_seconds`: Request duration histogram
- `proxy_active_connections`: Number of active connections
- `proxy_requests_by_status_class_total`: Requests grouped by status code class

### Grafana Dashboards

1. Access Grafana at http://localhost:3000
2. Login with credentials from `.env` (default: admin/admin)
3. Prometheus datasource is pre-configured
4. Import or create custom dashboards

See [MONITORING.md](MONITORING.md) for detailed monitoring setup and dashboard configuration.

## Testing

Load testing suite using k6 is available in the `tests/` directory:

```bash
cd tests
./run-all-tests.sh
```

## Project Structure

```
.
├── http-proxy/           # Main proxy server
│   ├── main.go          # Server entry point
│   ├── handlers/        # HTTP request handlers
│   ├── tunnel/          # HTTPS CONNECT tunneling
│   ├── blocklist/       # Domain blocking logic
│   ├── limit/           # Rate limiting (memory & Redis)
│   └── metrics/         # Prometheus metrics
├── tests/               # Load testing suite
├── grafana/             # Grafana provisioning
├── certs/               # TLS certificates
├── traffic-analytics/   # (In development)
├── zero-trust-gateway/  # (In development)
├── docker-compose.yml   # Monitoring stack
├── prometheus.yml       # Prometheus configuration
└── MONITORING.md        # Monitoring guide
```

## Performance

- Optimized connection pooling (500 max idle connections, 200 per host)
- Efficient bidirectional data transfer for HTTPS tunneling
- Redis EVALSHA optimization for distributed rate limiting
- Minimal memory footprint with periodic cleanup of stale limiters

## Roadmap

### Upcoming Features

- **Caching Layer**:
  - In-memory LRU cache
  - Persistent cache with configurable TTL
  - Distributed caching with Redis
  - Cache invalidation strategies

- **Load Balancing**:
  - Round-robin distribution
  - Least connections algorithm
  - Health checking
  - Automatic failover

- **Traffic Analytics**:
  - Request/response logging
  - Traffic pattern analysis
  - Bandwidth usage tracking
  - Geographic request distribution

- **Zero-Trust Gateway**:
  - JWT authentication
  - Policy-based access control
  - Request signing and validation

## Contributing

Contributions are welcome. Please follow standard Go conventions and include tests for new features.

## License

This project is licensed under the terms specified in the [LICENSE](LICENSE) file.

## Support

For issues, questions, or contributions, please visit the [GitHub repository](https://github.com/aluko123/go-network-proxy).
