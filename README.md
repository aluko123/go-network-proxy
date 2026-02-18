# go-network-proxy

A GPU-native LLM inference gateway and HTTP proxy written in Go. Optimized for self-hosted GPU inference with KV-cache-aware routing.

## Features

### Inference Gateway
- **Multi-backend routing** - OpenAI, Anthropic, local gRPC workers
- **Health-aware load balancing** - Routes based on GPU utilization and queue depth
- **Prefix-affinity routing** - Routes similar prompts to same worker for KV cache reuse
- **Priority queue** - High-priority requests processed first
- **SSE streaming** - Real-time token streaming to clients

### Forward Proxy (Optional)
- HTTP/HTTPS support (CONNECT tunneling)
- Domain blocking (exact + wildcard matching)
- Rate limiting (in-memory or Redis-based)
- Prometheus metrics + Grafana dashboards

## Quick Start

```bash
# Start infrastructure
cd deploy && docker-compose up -d

# Start mock workers
python workers/mock_server.py --model llama-70b --port 50051 &
python workers/mock_server.py --model llama-70b --port 50052 &

# Run the gateway
go run cmd/gateway/main.go -backends configs/backends.yaml

# Enable forward proxy handlers
go run cmd/gateway/main.go -backends configs/backends.yaml -enable-proxy
```

## API Reference

### POST /v1/inference

Stream LLM inference responses.

**Request:**
```json
{
  "prompt": "What is 2+2?",
  "prefix": "You are a helpful math tutor.",
  "model": "llama-70b",
  "max_tokens": 100,
  "temperature": 0.7,
  "priority": 5
}
```

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `prompt` | string | Yes | The user's input/question |
| `prefix` | string | No | Cacheable prefix (system prompt) for KV cache affinity |
| `model` | string | No | Model name, defaults to "default" |
| `max_tokens` | int | No | Max tokens to generate, default 100 |
| `temperature` | float | No | Sampling temperature, default 0.7 |
| `priority` | int | No | 1-10, higher = processed first |

**Response (SSE stream):**
```
data: {"request_id":"abc123","token":"The","token_count":1,"finished":false}
data: {"request_id":"abc123","token":" answer","token_count":2,"finished":false}
data: {"request_id":"abc123","token":" is","token_count":3,"finished":false}
data: {"request_id":"abc123","token":" 4","token_count":4,"finished":true}
```

**Headers:**
```
Authorization: Bearer <api-key>
Content-Type: application/json
```

### Prefix Affinity (KV Cache Optimization)

When you send a `prefix`, the gateway:
1. Hashes `model + prefix` to create a cache key
2. Tracks which workers have seen this prefix
3. Routes future requests with the same prefix to the same worker
4. Worker's GPU KV cache can reuse the prefix computation

**Best for:** System prompts, RAG context, few-shot examples that repeat across requests.

## Architecture

```
Client → Gateway → Priority Queue → Router → GPU Workers
           │                          │
           │                          ├─→ OpenAI API
           │                          ├─→ Anthropic API
           │                          └─→ gRPC Workers (local GPU)
           │
           └─ Prefix Index (tracks prefix → worker affinity)
```

## Configuration

| Flag | Default | Description |
|------|---------|-------------|
| `-backends` | "" | Path to backends.yaml config |
| `-router-workers` | 10 | Number of router worker goroutines |
| `-limiter` | redis | Rate limiter: memory or redis |
| `-rate-limit` | 100 | Requests per minute per IP |
| `-rate-burst` | 20 | Burst size |
| `-enable-proxy` | false | Enable forward proxy handlers |

## Metrics

Prometheus metrics at `/metrics`:

| Metric | Description |
|--------|-------------|
| `inference_requests_total` | Total requests by model/priority/status |
| `inference_time_to_first_token_seconds` | Time to first token histogram |
| `prefix_cache_hits_total` | Routing decisions using cached prefix |
| `prefix_cache_misses_total` | Routing decisions without cached prefix |
| `worker_gpu_utilization` | GPU utilization per worker |
| `worker_queue_depth` | Queue depth per worker |

## Testing

```bash
# Unit tests
go test ./...

# Integration tests
python3 tests/scripts/test-inference-gateway.py

# Load tests
python3 tests/scripts/load-test-inference.py --rps 50 --duration 30
```

## License

See [LICENSE](LICENSE).
