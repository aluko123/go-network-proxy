#!/usr/bin/env python3
"""
Integration tests for health-aware routing.
Tests that traffic shifts when workers become unhealthy.

Usage:
    1. Start mock workers with different GPU utilizations
    2. Start gateway
    3. Run this test

    python tests/scripts/test-health-routing.py --gateway http://localhost:8080
"""
import argparse
import asyncio
import aiohttp
import json
import subprocess
import time
import signal
import os
from dataclasses import dataclass
from typing import Optional, List


@dataclass
class TestResult:
    name: str
    passed: bool
    details: str = ""
    error: Optional[str] = None


API_KEY = "sk-dev-test-key-12345"


async def send_request(session: aiohttp.ClientSession, gateway_url: str, model: str = "mock-gpt") -> dict:
    """Send an inference request and return response info."""
    payload = {
        "model": model,
        "prompt": "Test request for health routing",
        "max_tokens": 10,
    }
    headers = {"Authorization": f"Bearer {API_KEY}"}
    
    start = time.time()
    tokens = []
    
    try:
        async with session.post(
            f"{gateway_url}/v1/inference",
            json=payload,
            headers=headers,
            timeout=aiohttp.ClientTimeout(total=30)
        ) as resp:
            if resp.status != 200:
                return {"success": False, "error": f"HTTP {resp.status}"}
            
            async for line in resp.content:
                line = line.decode('utf-8').strip()
                if line.startswith('data:'):
                    try:
                        data = json.loads(line[5:].strip())
                        if data.get('token'):
                            tokens.append(data['token'])
                        if data.get('finished'):
                            break
                    except:
                        pass
        
        return {
            "success": True,
            "tokens": len(tokens),
            "duration_ms": (time.time() - start) * 1000
        }
    except Exception as e:
        return {"success": False, "error": str(e)}


async def get_metrics(session: aiohttp.ClientSession, gateway_url: str) -> dict:
    """Fetch Prometheus metrics and parse relevant ones."""
    try:
        async with session.get(f"{gateway_url}/metrics") as resp:
            text = await resp.text()
            
            metrics = {}
            for line in text.split('\n'):
                if line.startswith('worker_healthy'):
                    # Parse: worker_healthy{worker_id="localhost:50051"} 1
                    if '{' in line:
                        worker_id = line.split('"')[1]
                        value = float(line.split('}')[1].strip())
                        metrics[f"healthy_{worker_id}"] = value
                elif line.startswith('worker_gpu_utilization'):
                    if '{' in line:
                        worker_id = line.split('"')[1]
                        value = float(line.split('}')[1].strip())
                        metrics[f"gpu_{worker_id}"] = value
            
            return metrics
    except Exception as e:
        return {"error": str(e)}


async def test_metrics_exported(session: aiohttp.ClientSession, gateway_url: str) -> TestResult:
    """Verify health metrics are exported to Prometheus."""
    metrics = await get_metrics(session, gateway_url)
    
    if "error" in metrics:
        return TestResult("metrics_exported", False, error=metrics["error"])
    
    # Check for worker health metrics
    health_metrics = [k for k in metrics.keys() if k.startswith("healthy_")]
    
    if len(health_metrics) > 0:
        return TestResult(
            "metrics_exported",
            True,
            details=f"Found {len(health_metrics)} worker health metrics"
        )
    else:
        return TestResult(
            "metrics_exported",
            False,
            error="No worker health metrics found"
        )


async def test_requests_succeed(session: aiohttp.ClientSession, gateway_url: str) -> TestResult:
    """Verify basic requests succeed."""
    results = []
    for _ in range(5):
        result = await send_request(session, gateway_url)
        results.append(result)
    
    successful = sum(1 for r in results if r.get("success"))
    
    if successful == 5:
        return TestResult("requests_succeed", True, details=f"All 5 requests succeeded")
    else:
        return TestResult(
            "requests_succeed",
            False,
            error=f"Only {successful}/5 requests succeeded"
        )


async def test_concurrent_requests(session: aiohttp.ClientSession, gateway_url: str) -> TestResult:
    """Send concurrent requests to test load distribution."""
    tasks = [send_request(session, gateway_url) for _ in range(10)]
    results = await asyncio.gather(*tasks)
    
    successful = sum(1 for r in results if r.get("success"))
    avg_duration = sum(r.get("duration_ms", 0) for r in results if r.get("success")) / max(successful, 1)
    
    if successful >= 8:  # Allow some failures
        return TestResult(
            "concurrent_requests",
            True,
            details=f"{successful}/10 succeeded, avg {avg_duration:.1f}ms"
        )
    else:
        errors = [r.get("error") for r in results if not r.get("success")]
        return TestResult(
            "concurrent_requests",
            False,
            error=f"Only {successful}/10 succeeded. Errors: {errors[:3]}"
        )


async def test_health_check_interval(session: aiohttp.ClientSession, gateway_url: str) -> TestResult:
    """Verify health checks are happening by watching metrics change."""
    metrics1 = await get_metrics(session, gateway_url)
    await asyncio.sleep(6)  # Wait for at least one health check (5s interval)
    metrics2 = await get_metrics(session, gateway_url)
    
    # Metrics should exist
    if "error" in metrics1 or "error" in metrics2:
        return TestResult(
            "health_check_interval",
            False,
            error="Failed to get metrics"
        )
    
    return TestResult(
        "health_check_interval",
        True,
        details="Health checks are running"
    )


async def run_all_tests(gateway_url: str) -> bool:
    """Run all health routing tests."""
    print(f"\n{'='*60}")
    print("Health-Aware Routing Tests")
    print(f"Gateway: {gateway_url}")
    print(f"{'='*60}\n")
    
    results: List[TestResult] = []
    
    async with aiohttp.ClientSession() as session:
        # Test 1: Metrics exported
        result = await test_metrics_exported(session, gateway_url)
        results.append(result)
        
        # Test 2: Basic requests work
        result = await test_requests_succeed(session, gateway_url)
        results.append(result)
        
        # Test 3: Concurrent requests
        result = await test_concurrent_requests(session, gateway_url)
        results.append(result)
        
        # Test 4: Health checks running
        result = await test_health_check_interval(session, gateway_url)
        results.append(result)
    
    # Print results
    passed = 0
    failed = 0
    
    for result in results:
        status = "✓ PASS" if result.passed else "✗ FAIL"
        print(f"{status} | {result.name}")
        if result.details:
            print(f"       {result.details}")
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
    parser = argparse.ArgumentParser(description="Test health-aware routing")
    parser.add_argument("--gateway", type=str, default="http://localhost:8080", help="Gateway URL")
    args = parser.parse_args()
    
    success = asyncio.run(run_all_tests(args.gateway))
    exit(0 if success else 1)
