#!/usr/bin/env python3
"""
Load test for the inference gateway.
Sends high volumes of concurrent requests to stress test the system.

Usage:
    1. Start multiple mock workers:
       python workers/mock_server.py --model gpt-small --port 50051 --latency 0.02 &
       python workers/mock_server.py --model gpt-small --port 50052 --latency 0.02 &
       python workers/mock_server.py --model gpt-large --port 50053 --latency 0.05 &

    2. Start the gateway with all workers:
       go run cmd/gateway/main.go \
         -worker-addrs "localhost:50051,localhost:50052,localhost:50053" \
         -limiter memory \
         -rate-limit 10000 \
         -rate-burst 1000

    3. Run load test:
       python tests/scripts/load-test-inference.py --rps 100 --duration 60
"""
import argparse
import asyncio
import aiohttp
import json
import time
import random
from dataclasses import dataclass, field
from typing import List
from collections import defaultdict


@dataclass
class LoadTestStats:
    total_requests: int = 0
    successful: int = 0
    failed: int = 0
    rate_limited: int = 0
    total_tokens: int = 0
    latencies: List[float] = field(default_factory=list)
    ttft_latencies: List[float] = field(default_factory=list)
    errors: List[str] = field(default_factory=list)
    requests_by_model: dict = field(default_factory=lambda: defaultdict(int))
    requests_by_priority: dict = field(default_factory=lambda: defaultdict(int))


SAMPLE_PROMPTS = [
    "Explain the theory of relativity in simple terms",
    "Write a haiku about programming",
    "What are the benefits of microservices architecture",
    "Describe the water cycle for a 5th grader",
    "List five tips for effective time management",
    "Explain how neural networks learn",
    "What is the difference between TCP and UDP",
    "Write a short poem about autumn leaves",
    "Explain quantum computing to a beginner",
    "What are the key principles of REST API design",
    "Describe photosynthesis in one paragraph",
    "What is the CAP theorem in distributed systems",
    "Explain the concept of recursion with an example",
    "What are the advantages of functional programming",
    "Describe the OSI model layers",
]

MODELS = ["gpt-small", "gpt-large", "default"]
PRIORITIES = [1, 2, 3, 5, 7, 10]


async def send_request(
    session: aiohttp.ClientSession,
    gateway_url: str,
    stats: LoadTestStats,
    request_id: int
) -> None:
    """Send a single inference request and record stats."""
    prompt = random.choice(SAMPLE_PROMPTS)
    model = random.choice(MODELS)
    priority = random.choice(PRIORITIES)
    
    payload = {
        "prompt": prompt,
        "priority": priority,
        "max_tokens": random.randint(20, 100),
        "model": model,
        "temperature": 0.7
    }
    
    start = time.time()
    first_token_time = None
    tokens_received = 0
    
    try:
        async with session.post(
            f"{gateway_url}/v1/inference",
            json=payload,
            timeout=aiohttp.ClientTimeout(total=120)
        ) as resp:
            if resp.status == 429:
                stats.rate_limited += 1
                stats.failed += 1
                return
            
            if resp.status != 200:
                stats.failed += 1
                stats.errors.append(f"HTTP {resp.status}")
                return
            
            async for line in resp.content:
                line = line.decode('utf-8').strip()
                if line.startswith('data:'):
                    if first_token_time is None:
                        first_token_time = time.time()
                    
                    try:
                        data = json.loads(line[5:].strip())
                        if data.get('token'):
                            tokens_received += 1
                        if data.get('token_count'):
                            tokens_received = data['token_count']
                        if data.get('finished'):
                            break
                    except json.JSONDecodeError:
                        pass
                elif line.startswith('event: error'):
                    stats.failed += 1
                    return
        
        end = time.time()
        latency = (end - start) * 1000
        
        stats.successful += 1
        stats.total_tokens += tokens_received
        stats.latencies.append(latency)
        stats.requests_by_model[model] += 1
        stats.requests_by_priority[priority] += 1
        
        if first_token_time:
            ttft = (first_token_time - start) * 1000
            stats.ttft_latencies.append(ttft)
            
    except asyncio.TimeoutError:
        stats.failed += 1
        stats.errors.append("Timeout")
    except Exception as e:
        stats.failed += 1
        stats.errors.append(str(e)[:50])


async def run_load_test(
    gateway_url: str,
    target_rps: int,
    duration_seconds: int,
    max_concurrent: int
) -> LoadTestStats:
    """Run the load test."""
    stats = LoadTestStats()
    
    connector = aiohttp.TCPConnector(limit=max_concurrent, limit_per_host=max_concurrent)
    async with aiohttp.ClientSession(connector=connector) as session:
        start_time = time.time()
        request_id = 0
        tasks = []
        
        interval = 1.0 / target_rps if target_rps > 0 else 0
        
        print(f"\n{'='*60}")
        print(f"Load Test Starting")
        print(f"Gateway: {gateway_url}")
        print(f"Target RPS: {target_rps}")
        print(f"Duration: {duration_seconds}s")
        print(f"Max Concurrent: {max_concurrent}")
        print(f"{'='*60}\n")
        
        last_report = start_time
        
        while time.time() - start_time < duration_seconds:
            # Launch a request
            request_id += 1
            stats.total_requests += 1
            task = asyncio.create_task(send_request(session, gateway_url, stats, request_id))
            tasks.append(task)
            
            # Clean up completed tasks periodically
            if len(tasks) > max_concurrent * 2:
                done = [t for t in tasks if t.done()]
                tasks = [t for t in tasks if not t.done()]
            
            # Progress report every 5 seconds
            now = time.time()
            if now - last_report >= 5:
                elapsed = now - start_time
                current_rps = stats.total_requests / elapsed
                print(f"[{elapsed:.0f}s] Sent: {stats.total_requests}, "
                      f"Success: {stats.successful}, Failed: {stats.failed}, "
                      f"RPS: {current_rps:.1f}")
                last_report = now
            
            # Rate limiting
            if interval > 0:
                await asyncio.sleep(interval)
        
        # Wait for remaining tasks
        print("\nWaiting for in-flight requests to complete...")
        if tasks:
            await asyncio.gather(*tasks, return_exceptions=True)
    
    return stats


def percentile(data: List[float], p: float) -> float:
    """Calculate percentile of a list."""
    if not data:
        return 0
    sorted_data = sorted(data)
    idx = int(len(sorted_data) * p / 100)
    return sorted_data[min(idx, len(sorted_data) - 1)]


def print_results(stats: LoadTestStats, duration: float):
    """Print load test results."""
    print(f"\n{'='*60}")
    print("LOAD TEST RESULTS")
    print(f"{'='*60}\n")
    
    print("Request Summary:")
    print(f"  Total Requests:  {stats.total_requests}")
    print(f"  Successful:      {stats.successful} ({stats.successful/max(stats.total_requests,1)*100:.1f}%)")
    print(f"  Failed:          {stats.failed}")
    print(f"  Rate Limited:    {stats.rate_limited}")
    print(f"  Total Tokens:    {stats.total_tokens}")
    print(f"  Actual RPS:      {stats.total_requests/duration:.1f}")
    
    if stats.latencies:
        print(f"\nLatency (end-to-end):")
        print(f"  p50:  {percentile(stats.latencies, 50):.1f}ms")
        print(f"  p95:  {percentile(stats.latencies, 95):.1f}ms")
        print(f"  p99:  {percentile(stats.latencies, 99):.1f}ms")
        print(f"  max:  {max(stats.latencies):.1f}ms")
    
    if stats.ttft_latencies:
        print(f"\nTime to First Token:")
        print(f"  p50:  {percentile(stats.ttft_latencies, 50):.1f}ms")
        print(f"  p95:  {percentile(stats.ttft_latencies, 95):.1f}ms")
        print(f"  p99:  {percentile(stats.ttft_latencies, 99):.1f}ms")
    
    print(f"\nRequests by Model:")
    for model, count in sorted(stats.requests_by_model.items()):
        print(f"  {model}: {count}")
    
    print(f"\nRequests by Priority:")
    for priority, count in sorted(stats.requests_by_priority.items()):
        label = "high" if priority >= 8 else "medium" if priority >= 4 else "low"
        print(f"  {priority} ({label}): {count}")
    
    if stats.errors:
        print(f"\nTop Errors:")
        error_counts = defaultdict(int)
        for e in stats.errors:
            error_counts[e] += 1
        for error, count in sorted(error_counts.items(), key=lambda x: -x[1])[:5]:
            print(f"  {error}: {count}")
    
    print(f"\n{'='*60}\n")


async def run_rate_limit_test(gateway_url: str, burst_size: int = 50) -> None:
    """Test rate limiting behavior by sending a burst of requests."""
    print(f"\n{'='*60}")
    print("RATE LIMIT TEST")
    print(f"Sending {burst_size} requests as fast as possible...")
    print(f"{'='*60}\n")
    
    connector = aiohttp.TCPConnector(limit=burst_size, limit_per_host=burst_size)
    async with aiohttp.ClientSession(connector=connector) as session:
        tasks = []
        start = time.time()
        
        for i in range(burst_size):
            payload = {
                "prompt": f"Rate limit test request {i}",
                "priority": 5,
                "max_tokens": 10,
                "model": "default",
            }
            tasks.append(session.post(
                f"{gateway_url}/v1/inference",
                json=payload,
                timeout=aiohttp.ClientTimeout(total=30)
            ))
        
        responses = await asyncio.gather(*tasks, return_exceptions=True)
        elapsed = time.time() - start
        
        success = 0
        rate_limited = 0
        errors = 0
        
        for resp in responses:
            if isinstance(resp, Exception):
                errors += 1
            else:
                if resp.status == 200:
                    success += 1
                elif resp.status == 429:
                    rate_limited += 1
                else:
                    errors += 1
                await resp.release()
        
        print(f"Results ({elapsed*1000:.0f}ms elapsed):")
        print(f"  Successful:    {success}")
        print(f"  Rate Limited:  {rate_limited}")
        print(f"  Errors:        {errors}")
        print(f"\n{'='*60}\n")


async def main():
    parser = argparse.ArgumentParser(description="Load test the inference gateway")
    parser.add_argument("--gateway", type=str, default="http://localhost:8080", help="Gateway URL")
    parser.add_argument("--rps", type=int, default=50, help="Target requests per second")
    parser.add_argument("--duration", type=int, default=30, help="Test duration in seconds")
    parser.add_argument("--concurrent", type=int, default=200, help="Max concurrent connections")
    parser.add_argument("--test-rate-limit", action="store_true", help="Run rate limit burst test")
    parser.add_argument("--burst-size", type=int, default=50, help="Number of requests for rate limit test")
    args = parser.parse_args()
    
    if args.test_rate_limit:
        await run_rate_limit_test(args.gateway, args.burst_size)
    else:
        start = time.time()
        stats = await run_load_test(args.gateway, args.rps, args.duration, args.concurrent)
        duration = time.time() - start
        print_results(stats, duration)


if __name__ == "__main__":
    asyncio.run(main())
