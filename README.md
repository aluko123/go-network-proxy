# go-network-proxy

A high-performance HTTP/HTTPS forward proxy and LLM inference gateway written in Go.

## Features

### Forward Proxy
- HTTP/HTTPS support (CONNECT tunneling)
- Domain blocking (exact + wildcard matching)
- Rate limiting (in-memory or Redis-based leaky bucket)
- Prometheus metrics + Grafana dashboards

### Inference Gateway
- Priority queue for LLM requests
- gRPC streaming to Python workers
- SSE response streaming to clients

### In Development
- Model routing (small vs large models)
- Request coalescing (dedupe identical prompts)
- Prefix caching (KV reuse for common prompts)

## Quick Start

```bash
# Start infrastructure
cd deploy && docker-compose up -d

# Run the gateway
go run cmd/gateway/main.go

# With inference workers
go run cmd/gateway/main.go -worker-addrs "localhost:50051,localhost:50052"
```

## Architecture

```
┌─────────────┐     ┌─────────────────────────────────────────────────┐
│   Client    │     │                  Go Gateway                     │
│  (HTTP)     │────▶│  ┌──────────┐   ┌──────────┐   ┌─────────────┐  │
└─────────────┘     │  │ Rate     │──▶│ Priority │──▶│   Router    │  │
                    │  │ Limiter  │   │  Queue   │   │             │  │
┌─────────────┐     │  └──────────┘   └──────────┘   └──────┬──────┘  │
│   Client    │────▶│                                       │         │
│  (SSE)      │◀────│───────────────────────────────────────┘         │
└─────────────┘     └─────────────────────────────────────────────────┘
                                                            │
                              ┌──────────────────────────────┼──────────────────────────────┐
                              ▼                              ▼                              ▼
                    ┌──────────────────┐          ┌──────────────────┐          ┌──────────────────┐
                    │  Python Worker   │          │  Python Worker   │          │  Python Worker   │
                    │  (gRPC Stream)   │          │  (gRPC Stream)   │          │  (gRPC Stream)   │
                    │  :50051          │          │  :50052          │          │  :50053          │
                    └──────────────────┘          └──────────────────┘          └──────────────────┘
```

**Flow:** HTTP request → Blocklist for IPs → Rate limit check → Priority queue (high priority first) → Router dispatches to available worker → gRPC streaming → SSE response to client

## Configuration

| Flag | Default | Description |
|------|---------|-------------|
| `-proto` | http | Protocol: http or https |
| `-limiter` | redis | Rate limiter: memory or redis |
| `-redis-addr` | localhost:6379 | Redis address |
| `-rate-limit` | 100 | Requests per minute per IP |
| `-rate-burst` | 20 | Burst size |
| `-worker-addrs` | "" | Comma-separated worker addresses |
| `-read-timeout` | 30s | HTTP read timeout |
| `-write-timeout` | 60s | HTTP write timeout |
| `-idle-timeout` | 120s | HTTP idle timeout |
| `-inference-timeout` | 5m | Max inference request duration |
| `-shutdown-timeout` | 30s | Graceful shutdown timeout |

## Project Structure

```
├── cmd/gateway/        # Entry point
├── proxy/              # Forward proxy (handlers, tunnel)
├── inference/          # LLM gateway (queue, router, worker)
├── pkg/                # Shared libs (blocklist, limit, metrics, middleware)
├── workers/            # Python gRPC workers
├── tests/              # k6 load tests + integration scripts
└── deploy/             # Docker compose + Prometheus
```

## Testing

```bash
# Unit tests
go test ./...

# Integration tests (start gateway + workers first)
python3 tests/scripts/test-inference-gateway.py

# Load tests
cd tests && ./run-all-tests.sh
```

## License

See [LICENSE](LICENSE).
