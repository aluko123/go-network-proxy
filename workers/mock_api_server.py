#!/usr/bin/env python3
"""
Mock API server that mimics OpenAI and Anthropic streaming endpoints.
Use this to test model routing without real API keys.

Usage:
    python workers/mock_api_server.py --port 8000

Then configure backends.yaml to point to http://localhost:8000
"""
import argparse
import asyncio
import json
import time
import uuid
from aiohttp import web


class MockAPIServer:
    def __init__(self, latency: float = 0.02):
        self.latency = latency

    async def openai_chat_completions(self, request: web.Request) -> web.StreamResponse:
        """Mock OpenAI /v1/chat/completions endpoint with SSE streaming."""
        try:
            body = await request.json()
        except:
            return web.Response(status=400, text="Invalid JSON")

        model = body.get("model", "gpt-4")
        messages = body.get("messages", [])
        max_tokens = body.get("max_tokens", 100)
        stream = body.get("stream", False)

        prompt = ""
        for msg in messages:
            if msg.get("role") == "user":
                prompt = msg.get("content", "")
                break

        # Generate mock response tokens
        response_text = f"[Mock {model}] You said: {prompt[:50]}... Here's a helpful response with some generated tokens."
        tokens = response_text.split()[:max_tokens]

        if not stream:
            # Non-streaming response
            return web.json_response({
                "id": f"chatcmpl-{uuid.uuid4().hex[:8]}",
                "object": "chat.completion",
                "created": int(time.time()),
                "model": model,
                "choices": [{
                    "index": 0,
                    "message": {"role": "assistant", "content": " ".join(tokens)},
                    "finish_reason": "stop"
                }]
            })

        # Streaming response
        response = web.StreamResponse(
            status=200,
            headers={
                "Content-Type": "text/event-stream",
                "Cache-Control": "no-cache",
                "Connection": "keep-alive",
            }
        )
        await response.prepare(request)

        chat_id = f"chatcmpl-{uuid.uuid4().hex[:8]}"

        for token in tokens:
            chunk = {
                "id": chat_id,
                "object": "chat.completion.chunk",
                "created": int(time.time()),
                "model": model,
                "choices": [{
                    "index": 0,
                    "delta": {"content": token + " "},
                    "finish_reason": None
                }]
            }
            await response.write(f"data: {json.dumps(chunk)}\n\n".encode())
            await asyncio.sleep(self.latency)

        # Send final chunk
        final_chunk = {
            "id": chat_id,
            "object": "chat.completion.chunk",
            "created": int(time.time()),
            "model": model,
            "choices": [{
                "index": 0,
                "delta": {},
                "finish_reason": "stop"
            }]
        }
        await response.write(f"data: {json.dumps(final_chunk)}\n\n".encode())
        await response.write(b"data: [DONE]\n\n")

        return response

    async def anthropic_messages(self, request: web.Request) -> web.StreamResponse:
        """Mock Anthropic /v1/messages endpoint with SSE streaming."""
        try:
            body = await request.json()
        except:
            return web.Response(status=400, text="Invalid JSON")

        model = body.get("model", "claude-3-sonnet")
        messages = body.get("messages", [])
        max_tokens = body.get("max_tokens", 100)
        stream = body.get("stream", False)

        prompt = ""
        for msg in messages:
            if msg.get("role") == "user":
                prompt = msg.get("content", "")
                break

        # Generate mock response tokens
        response_text = f"[Mock {model}] I understand you said: {prompt[:50]}... Let me provide a thoughtful response."
        tokens = response_text.split()[:max_tokens]

        if not stream:
            return web.json_response({
                "id": f"msg_{uuid.uuid4().hex[:8]}",
                "type": "message",
                "role": "assistant",
                "content": [{"type": "text", "text": " ".join(tokens)}],
                "model": model,
                "stop_reason": "end_turn",
                "usage": {"input_tokens": len(prompt.split()), "output_tokens": len(tokens)}
            })

        # Streaming response
        response = web.StreamResponse(
            status=200,
            headers={
                "Content-Type": "text/event-stream",
                "Cache-Control": "no-cache",
            }
        )
        await response.prepare(request)

        msg_id = f"msg_{uuid.uuid4().hex[:8]}"

        # Message start
        await response.write(f"event: message_start\n".encode())
        await response.write(f"data: {json.dumps({'type': 'message_start', 'message': {'id': msg_id, 'type': 'message', 'role': 'assistant', 'model': model}})}\n\n".encode())

        # Content block start
        await response.write(f"event: content_block_start\n".encode())
        await response.write(f"data: {json.dumps({'type': 'content_block_start', 'index': 0, 'content_block': {'type': 'text', 'text': ''}})}\n\n".encode())

        # Stream tokens
        for token in tokens:
            await response.write(f"event: content_block_delta\n".encode())
            delta = {
                "type": "content_block_delta",
                "index": 0,
                "delta": {"type": "text_delta", "text": token + " "}
            }
            await response.write(f"data: {json.dumps(delta)}\n\n".encode())
            await asyncio.sleep(self.latency)

        # Content block stop
        await response.write(f"event: content_block_stop\n".encode())
        await response.write(f"data: {json.dumps({'type': 'content_block_stop', 'index': 0})}\n\n".encode())

        # Message delta (usage)
        await response.write(f"event: message_delta\n".encode())
        await response.write(f"data: {json.dumps({'type': 'message_delta', 'delta': {'stop_reason': 'end_turn'}, 'usage': {'output_tokens': len(tokens)}})}\n\n".encode())

        # Message stop
        await response.write(f"event: message_stop\n".encode())
        await response.write(f"data: {json.dumps({'type': 'message_stop'})}\n\n".encode())

        return response

    async def health(self, request: web.Request) -> web.Response:
        return web.json_response({"status": "ok", "endpoints": ["/v1/chat/completions", "/v1/messages"]})


def create_app(latency: float = 0.02) -> web.Application:
    server = MockAPIServer(latency=latency)
    app = web.Application()
    app.router.add_post("/v1/chat/completions", server.openai_chat_completions)
    app.router.add_post("/v1/messages", server.anthropic_messages)
    app.router.add_get("/health", server.health)
    app.router.add_get("/", server.health)
    return app


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description="Mock OpenAI/Anthropic API server")
    parser.add_argument("--port", type=int, default=8000, help="Port to listen on")
    parser.add_argument("--latency", type=float, default=0.02, help="Latency per token in seconds")
    args = parser.parse_args()

    print(f"Starting Mock API Server on http://localhost:{args.port}")
    print(f"  OpenAI endpoint:    POST /v1/chat/completions")
    print(f"  Anthropic endpoint: POST /v1/messages")
    print(f"  Token latency:      {args.latency}s")

    app = create_app(latency=args.latency)
    web.run_app(app, port=args.port, print=lambda x: None)
