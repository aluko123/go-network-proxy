# Roadmap

## Project Vision

**GPU-native inference gateway for self-hosted LLMs** - optimized for running your own models efficiently, not API proxying.

Differentiators from LiteLLM/general proxies:
- KV-cache-aware routing (prefix affinity)
- GPU memory and utilization-aware scheduling
- NVIDIA-specific metrics and monitoring
- Designed for Kubernetes GPU clusters

---

## Phase 1: Foundation (Hardening) ✅

- [X] Structured logging - JSON logs with request IDs
- [X] Configurable timeouts - Flag for request timeout
- [X] Graceful shutdown - Drain queue on SIGTERM
- [X] Basic inference metrics - Queue depth, request latency, worker status
- [X] Auth on `/v1/inference` - API key validation

## Phase 2: Core Features

- [X] Model routing - Route by model name to specific backends
- [X] Health-aware routing - Periodic health checks, GPU-aware worker selection
- [ ] ~~Request coalescing~~ - *Skipped: prefix caching more impactful*

## Phase 3: GPU-Native Routing

- [X] **Prefix-affinity routing** - Route requests with same prefix to same worker for KV cache reuse
- [ ] **GPU memory-aware routing** - Don't send large contexts to memory-constrained workers
- [X] **vLLM backend** - HTTP backend for vLLM servers (OpenAI-compatible API)
- [ ] Circuit breakers - Back off failing workers (GPU-aware: OOM, CUDA errors, thermal)

## Phase 4: NVIDIA Integration

- [ ] **DCGM metrics integration** - SM utilization, memory bandwidth, NVLink stats
- [ ] **Multi-GPU awareness** - Tensor parallelism topology, NVLink-aware placement
- [ ] **Batch scheduling** - Collect requests for batched dispatch to workers
- [ ] Distributed tracing - OpenTelemetry integration

## Phase 5: Production Readiness

- [ ] Kubernetes operator - GPU-aware pod scheduling integration
- [ ] Helm chart with GPU node selectors
- [ ] Horizontal scaling - Stateless gateway with shared prefix index (Redis)
- [ ] Admin dashboard - Worker status, GPU metrics, cache hit rates
- [ ] API key management - Hot reload, per-key rate limits, usage tracking

---

## Deprecated / Low Priority

### API Provider Backends (OpenAI, Anthropic)

**Status:** Functional but not strategic

The OpenAI and Anthropic backends work, but they don't align with the project's GPU-native focus:
- No prefix caching benefit (we don't control their routing)
- No GPU metrics (black box APIs)
- LiteLLM already does this better with 100+ providers

**Recommendation:** Keep for testing/fallback, but don't invest further. Mark as "community maintained" if project grows.

Files:
- `inference/backend/openai.go` - Works, no further development
- `inference/backend/anthropic.go` - Works, no further development

---

## Reference Projects

| Project | What to Learn |
|---------|---------------|
| [vLLM](https://github.com/vllm-project/vllm) | PagedAttention, prefix caching, continuous batching |
| [TGI](https://github.com/huggingface/text-generation-inference) | Production serving, batching, multi-GPU |
| [NVIDIA Triton](https://github.com/triton-inference-server/server) | Model serving, batching, metrics |
| [DCGM](https://github.com/NVIDIA/DCGM) | GPU metrics collection |
| [Envoy](https://github.com/envoyproxy/envoy) | Circuit breakers, health checks, observability |
| [LiteLLM](https://github.com/BerriAI/litellm) | API proxy patterns (what NOT to compete on) |
