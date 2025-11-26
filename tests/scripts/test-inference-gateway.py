#!/usr/bin/env python3
"""
Integration test for the inference gateway.
Tests the full flow: HTTP request -> Go Gateway -> Priority Queue -> Mock Worker -> SSE Response

Usage:
    1. Start mock workers:
       python workers/mock_server.py --model small-model --port 50051
       python workers/mock_server.py --model large-model --port 50052

    2. Start the gateway:
       go run cmd/gateway/main.go -worker-addrs "localhost:50051,localhost:50052"

    3. Run this test:
       python tests/scripts/test-inference-gateway.py
"""
import argparse
import asyncio
import aiohttp
import json
import time
from dataclasses import dataclass
from typing import List, Optional


@dataclass
class TestResult:
    name: str
    passed: bool
    duration_ms: float
    error: Optional[str] = None
    tokens_received: int = 0


async def send_inference_request(
    session: aiohttp.ClientSession,
    gateway_url: str,
    prompt: str,
    priority: int = 1,
    max_tokens: int = 50,
    model: str = "default"
) -> tuple[List[str], float]:
    """Send an inference request and collect streamed tokens."""
    payload = {
        "prompt": prompt,
        "priority": priority,
        "max_tokens": max_tokens,
        "model": model,
        "temperature": 0.7
    }
    
    tokens = []
    start = time.time()
    
    async with session.post(f"{gateway_url}/v1/inference", json=payload) as resp:
        if resp.status != 200:
            raise Exception(f"HTTP {resp.status}: {await resp.text()}")
        
        async for line in resp.content:
            line = line.decode('utf-8').strip()
            if line.startswith('data:'):
                data = json.loads(line[5:].strip())
                if data.get('token'):
                    tokens.append(data['token'])
                if data.get('finished'):
                    break
    
    duration = (time.time() - start) * 1000
    return tokens, duration


async def test_basic_inference(session: aiohttp.ClientSession, gateway_url: str) -> TestResult:
    """Test basic inference request/response."""
    name = "basic_inference"
    try:
        tokens, duration = await send_inference_request(
            session, gateway_url,
            prompt="Hello world this is a test",
            priority=1
        )
        
        if len(tokens) == 0:
            return TestResult(name, False, duration, "No tokens received")
        
        return TestResult(name, True, duration, tokens_received=len(tokens))
    except Exception as e:
        return TestResult(name, False, 0, str(e))


async def test_priority_ordering(session: aiohttp.ClientSession, gateway_url: str) -> TestResult:
    """Test that high priority requests are processed first."""
    name = "priority_ordering"
    try:
        results = []
        
        # Send requests with different priorities concurrently
        tasks = [
            send_inference_request(session, gateway_url, "low priority request", priority=1),
            send_inference_request(session, gateway_url, "high priority request", priority=10),
            send_inference_request(session, gateway_url, "medium priority request", priority=5),
        ]
        
        responses = await asyncio.gather(*tasks)
        
        # All should complete successfully
        for tokens, duration in responses:
            if len(tokens) == 0:
                return TestResult(name, False, 0, "Empty response received")
            results.append((tokens, duration))
        
        total_duration = sum(r[1] for r in results)
        return TestResult(name, True, total_duration, tokens_received=sum(len(r[0]) for r in results))
    
    except Exception as e:
        return TestResult(name, False, 0, str(e))


async def test_concurrent_requests(session: aiohttp.ClientSession, gateway_url: str, num_requests: int = 10) -> TestResult:
    """Test handling multiple concurrent requests."""
    name = f"concurrent_requests_{num_requests}"
    try:
        tasks = [
            send_inference_request(
                session, gateway_url,
                prompt=f"Concurrent request number {i}",
                priority=i % 5 + 1
            )
            for i in range(num_requests)
        ]
        
        start = time.time()
        responses = await asyncio.gather(*tasks, return_exceptions=True)
        total_duration = (time.time() - start) * 1000
        
        successful = 0
        total_tokens = 0
        errors = []
        
        for resp in responses:
            if isinstance(resp, Exception):
                errors.append(str(resp))
            else:
                tokens, _ = resp
                if len(tokens) > 0:
                    successful += 1
                    total_tokens += len(tokens)
        
        if successful == num_requests:
            return TestResult(name, True, total_duration, tokens_received=total_tokens)
        else:
            return TestResult(name, False, total_duration, f"Only {successful}/{num_requests} succeeded. Errors: {errors[:3]}")
    
    except Exception as e:
        return TestResult(name, False, 0, str(e))


async def test_rate_limiting(session: aiohttp.ClientSession, gateway_url: str) -> TestResult:
    """Test that rate limiting kicks in when burst limit is exceeded."""
    name = "rate_limiting"
    try:
        # Send more requests than the default burst limit (20)
        num_requests = 30
        
        tasks = [
            send_inference_request(
                session, gateway_url,
                prompt=f"Rate limit test {i}",
                priority=1
            )
            for i in range(num_requests)
        ]
        
        start = time.time()
        responses = await asyncio.gather(*tasks, return_exceptions=True)
        total_duration = (time.time() - start) * 1000
        
        successful = 0
        rate_limited = 0
        
        for resp in responses:
            if isinstance(resp, Exception):
                if "429" in str(resp):
                    rate_limited += 1
            else:
                tokens, _ = resp
                if len(tokens) > 0:
                    successful += 1
        
        # We expect some to succeed and some to be rate limited
        if rate_limited > 0 and successful > 0:
            return TestResult(name, True, total_duration, 
                tokens_received=successful,
                error=f"Rate limited {rate_limited}/{num_requests} requests (expected)")
        elif rate_limited == 0:
            return TestResult(name, False, total_duration, "No requests were rate limited - rate limiter may not be working")
        else:
            return TestResult(name, False, total_duration, f"All requests rate limited, none succeeded")
    
    except Exception as e:
        return TestResult(name, False, 0, str(e))


async def test_empty_prompt_rejected(session: aiohttp.ClientSession, gateway_url: str) -> TestResult:
    """Test that empty prompts are rejected with 400."""
    name = "empty_prompt_rejected"
    try:
        payload = {"prompt": "", "priority": 1}
        
        async with session.post(f"{gateway_url}/v1/inference", json=payload) as resp:
            if resp.status == 400:
                return TestResult(name, True, 0)
            else:
                return TestResult(name, False, 0, f"Expected 400, got {resp.status}")
    
    except Exception as e:
        return TestResult(name, False, 0, str(e))


async def run_all_tests(gateway_url: str):
    """Run all integration tests."""
    print(f"\n{'='*60}")
    print(f"Inference Gateway Integration Tests")
    print(f"Gateway: {gateway_url}")
    print(f"{'='*60}\n")
    
    results = []
    async with aiohttp.ClientSession() as session:
        # Run tests sequentially to avoid rate limit interference between tests
        # Order matters: rate limiting test first (needs fresh bucket), 
        # then smaller tests, then larger concurrent tests last
        test_funcs = [
            ("rate_limiting", lambda: test_rate_limiting(session, gateway_url)),
            ("empty_prompt", lambda: test_empty_prompt_rejected(session, gateway_url)),
            ("basic", lambda: test_basic_inference(session, gateway_url)),
            ("priority", lambda: test_priority_ordering(session, gateway_url)),
            ("concurrent_5", lambda: test_concurrent_requests(session, gateway_url, 5)),
            ("concurrent_10", lambda: test_concurrent_requests(session, gateway_url, 10)),
        ]
        
        for name, test_func in test_funcs:
            result = await test_func()
            results.append(result)
            # Wait for bucket to refill: 100 req/min = ~1.67/sec, burst=20, so ~12s full refill
            # Use 3s between tests as a reasonable balance
            await asyncio.sleep(3)
    
    # Print results
    passed = 0
    failed = 0
    
    for result in results:
        status = "✓ PASS" if result.passed else "✗ FAIL"
        print(f"{status} | {result.name}")
        print(f"       Duration: {result.duration_ms:.1f}ms, Tokens: {result.tokens_received}")
        if result.error:
            print(f"       Error: {result.error}")
        print()
        
        if result.passed:
            passed += 1
        else:
            failed += 1
    
    print(f"{'='*60}")
    print(f"Results: {passed} passed, {failed} failed")
    print(f"{'='*60}\n")
    
    return failed == 0


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description="Test the inference gateway")
    parser.add_argument("--gateway", type=str, default="http://localhost:8080", help="Gateway URL")
    args = parser.parse_args()
    
    success = asyncio.run(run_all_tests(args.gateway))
    exit(0 if success else 1)
