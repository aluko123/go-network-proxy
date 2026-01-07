# Roadmap

## Phase 1: Foundation (Hardening)

- [X] Structured logging - JSON logs with request IDs
- [X] Configurable timeouts - Flag for request timeout
- [X] Graceful shutdown - Drain queue on SIGTERM
- [X] Basic inference metrics - Queue depth, request latency, worker status
- [X] Auth on `/v1/inference` - API key validation

## Phase 2: Core Features

- [X] Model routing - Route by model name to specific backends (OpenAI, Anthropic, gRPC)
- [X] Health-aware routing - Periodic health checks, GPU-aware worker selection
- [ ] Request coalescing - Dedupe identical prompts in-flight

## Phase 3: Advanced

- [ ] Prefix caching - Route similar prompts to same worker
- [ ] Circuit breakers - Back off failing workers
- [ ] Distributed tracing - OpenTelemetry integration

---

## Reference Projects

| Project | Description |
|---------|-------------|
| [LiteLLM](https://github.com/BerriAI/litellm) | Python LLM proxy - routing, auth, rate limiting |
| [OpenLLM](https://github.com/bentoml/OpenLLM) | LLM serving with load balancing |
| [Envoy](https://github.com/envoyproxy/envoy) | Circuit breakers, health checks, observability |
| [Traefik](https://github.com/traefik/traefik) | Middleware chain, dynamic config, metrics |
