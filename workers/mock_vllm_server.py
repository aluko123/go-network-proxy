#!/usr/bin/env python3
"""
Mock vLLM server for development/testing.
Exposes OpenAI-compatible API without requiring actual vLLM or GPU.

Usage:
    pip install fastapi uvicorn
    python workers/mock_vllm_server.py --port 8000 --model llama-70b

Endpoints:
    POST /v1/chat/completions - OpenAI-compatible streaming
    GET  /health              - Health check with mock GPU stats
    GET  /v1/models           - List available models
"""
import argparse
import asyncio
import json
import time
import random
import logging
from typing import List, Optional
from dataclasses import dataclass

from fastapi import FastAPI, Request
from fastapi.responses import StreamingResponse, JSONResponse
import uvicorn

logging.basicConfig(level=logging.INFO, format='%(asctime)s [%(levelname)s] %(message)s')
logger = logging.getLogger(__name__)

app = FastAPI(title="Mock vLLM Server")

# Server state
@dataclass
class ServerState:
    model_name: str = "mock-llama"
    latency_per_token: float = 0.02
    requests_running: int = 0
    total_requests: int = 0

state = ServerState()


@app.get("/health")
async def health():
    """Health endpoint with mock GPU stats."""
    return {
        "status": "healthy",
        "gpu_memory_used": random.randint(10_000_000_000, 20_000_000_000),  # 10-20GB
        "gpu_memory_total": 24_000_000_000,  # 24GB
        "kv_cache_usage": random.uniform(0.1, 0.6),
        "requests_running": state.requests_running,
    }


@app.get("/v1/models")
async def list_models():
    """List available models (OpenAI-compatible)."""
    return {
        "object": "list",
        "data": [
            {
                "id": state.model_name,
                "object": "model",
                "created": int(time.time()),
                "owned_by": "mock-vllm",
            }
        ]
    }


@app.post("/v1/chat/completions")
async def chat_completions(request: Request):
    """OpenAI-compatible chat completions with streaming."""
    body = await request.json()
    
    model = body.get("model", state.model_name)
    messages = body.get("messages", [])
    max_tokens = body.get("max_tokens", 100)
    stream = body.get("stream", False)
    
    # Extract prompt from messages
    prompt_parts = []
    for msg in messages:
        role = msg.get("role", "user")
        content = msg.get("content", "")
        prompt_parts.append(f"[{role}] {content}")
    
    full_prompt = " ".join(prompt_parts)
    logger.info(f"[{model}] Request: {full_prompt[:80]}...")
    
    state.requests_running += 1
    state.total_requests += 1
    request_id = f"chatcmpl-mock-{state.total_requests}"
    
    try:
        if stream:
            return StreamingResponse(
                generate_stream(request_id, model, full_prompt, max_tokens),
                media_type="text/event-stream"
            )
        else:
            # Non-streaming response
            tokens = generate_mock_tokens(full_prompt, max_tokens)
            return {
                "id": request_id,
                "object": "chat.completion",
                "created": int(time.time()),
                "model": model,
                "choices": [{
                    "index": 0,
                    "message": {
                        "role": "assistant",
                        "content": " ".join(tokens)
                    },
                    "finish_reason": "stop"
                }],
                "usage": {
                    "prompt_tokens": len(full_prompt.split()),
                    "completion_tokens": len(tokens),
                    "total_tokens": len(full_prompt.split()) + len(tokens)
                }
            }
    finally:
        state.requests_running -= 1


def generate_mock_tokens(prompt: str, max_tokens: int) -> List[str]:
    """Generate mock response tokens based on prompt."""
    # Simple mock: echo some words and add filler
    words = prompt.split()[:5]
    filler = ["This", "is", "a", "mock", "response", "from", "vLLM", "server", "."]
    tokens = [f"[{state.model_name}]"] + words + filler
    return tokens[:max_tokens]


async def generate_stream(request_id: str, model: str, prompt: str, max_tokens: int):
    """Generate SSE stream of tokens."""
    tokens = generate_mock_tokens(prompt, max_tokens)
    
    for i, token in enumerate(tokens):
        await asyncio.sleep(state.latency_per_token)
        
        chunk = {
            "id": request_id,
            "object": "chat.completion.chunk",
            "created": int(time.time()),
            "model": model,
            "choices": [{
                "index": 0,
                "delta": {"content": token + " "},
                "finish_reason": None
            }]
        }
        yield f"data: {json.dumps(chunk)}\n\n"
    
    # Final chunk with finish_reason
    final_chunk = {
        "id": request_id,
        "object": "chat.completion.chunk",
        "created": int(time.time()),
        "model": model,
        "choices": [{
            "index": 0,
            "delta": {},
            "finish_reason": "stop"
        }]
    }
    yield f"data: {json.dumps(final_chunk)}\n\n"
    yield "data: [DONE]\n\n"
    
    logger.info(f"[{model}] Completed {request_id}: {len(tokens)} tokens")


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description="Mock vLLM server")
    parser.add_argument("--port", type=int, default=8000, help="Port to listen on")
    parser.add_argument("--model", type=str, default="mock-llama", help="Model name to report")
    parser.add_argument("--latency", type=float, default=0.02, help="Latency per token (seconds)")
    args = parser.parse_args()
    
    state.model_name = args.model
    state.latency_per_token = args.latency
    
    logger.info(f"Starting Mock vLLM Server on port {args.port}")
    logger.info(f"Model: {args.model}, Latency: {args.latency}s/token")
    
    uvicorn.run(app, host="0.0.0.0", port=args.port, log_level="warning")
