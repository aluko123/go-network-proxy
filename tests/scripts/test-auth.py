#!/usr/bin/env python3
"""
Auth integration tests for the inference gateway.
Tests API key authentication on /v1/inference endpoint.

Usage:
    python tests/scripts/test-auth.py --gateway http://localhost:8080
"""
import argparse
import asyncio
import aiohttp
from dataclasses import dataclass
from typing import Optional


@dataclass
class TestResult:
    name: str
    passed: bool
    error: Optional[str] = None


VALID_API_KEY = "sk-dev-test-key-12345"
INVALID_API_KEY = "sk-invalid-key-00000"


async def test_no_auth_header(session: aiohttp.ClientSession, gateway_url: str) -> TestResult:
    """Request without Authorization header should return 401."""
    name = "no_auth_header_returns_401"
    try:
        payload = {"prompt": "test", "priority": 1, "max_tokens": 10}
        async with session.post(f"{gateway_url}/v1/inference", json=payload) as resp:
            if resp.status == 401:
                return TestResult(name, True)
            else:
                return TestResult(name, False, f"Expected 401, got {resp.status}")
    except Exception as e:
        return TestResult(name, False, str(e))


async def test_invalid_api_key(session: aiohttp.ClientSession, gateway_url: str) -> TestResult:
    """Request with invalid API key should return 401."""
    name = "invalid_api_key_returns_401"
    try:
        payload = {"prompt": "test", "priority": 1, "max_tokens": 10}
        headers = {"Authorization": f"Bearer {INVALID_API_KEY}"}
        async with session.post(f"{gateway_url}/v1/inference", json=payload, headers=headers) as resp:
            if resp.status == 401:
                return TestResult(name, True)
            else:
                return TestResult(name, False, f"Expected 401, got {resp.status}")
    except Exception as e:
        return TestResult(name, False, str(e))


async def test_malformed_auth_header(session: aiohttp.ClientSession, gateway_url: str) -> TestResult:
    """Request with malformed Authorization header should return 401."""
    name = "malformed_auth_header_returns_401"
    try:
        payload = {"prompt": "test", "priority": 1, "max_tokens": 10}
        # Missing "Bearer" prefix
        headers = {"Authorization": VALID_API_KEY}
        async with session.post(f"{gateway_url}/v1/inference", json=payload, headers=headers) as resp:
            if resp.status == 401:
                return TestResult(name, True)
            else:
                return TestResult(name, False, f"Expected 401, got {resp.status}")
    except Exception as e:
        return TestResult(name, False, str(e))


async def test_empty_bearer_token(session: aiohttp.ClientSession, gateway_url: str) -> TestResult:
    """Request with empty Bearer token should return 401."""
    name = "empty_bearer_token_returns_401"
    try:
        payload = {"prompt": "test", "priority": 1, "max_tokens": 10}
        headers = {"Authorization": "Bearer "}
        async with session.post(f"{gateway_url}/v1/inference", json=payload, headers=headers) as resp:
            if resp.status == 401:
                return TestResult(name, True)
            else:
                return TestResult(name, False, f"Expected 401, got {resp.status}")
    except Exception as e:
        return TestResult(name, False, str(e))


async def test_valid_api_key(session: aiohttp.ClientSession, gateway_url: str) -> TestResult:
    """Request with valid API key should return 200 and stream tokens."""
    name = "valid_api_key_returns_200"
    try:
        payload = {"prompt": "Hello world test", "priority": 1, "max_tokens": 10}
        headers = {"Authorization": f"Bearer {VALID_API_KEY}"}
        async with session.post(f"{gateway_url}/v1/inference", json=payload, headers=headers) as resp:
            if resp.status != 200:
                return TestResult(name, False, f"Expected 200, got {resp.status}")
            
            # Verify we get SSE response
            content = await resp.text()
            if "data:" in content:
                return TestResult(name, True)
            else:
                return TestResult(name, False, "No SSE data received")
    except Exception as e:
        return TestResult(name, False, str(e))


async def test_case_insensitive_bearer(session: aiohttp.ClientSession, gateway_url: str) -> TestResult:
    """Bearer prefix should be case-insensitive."""
    name = "bearer_case_insensitive"
    try:
        payload = {"prompt": "test", "priority": 1, "max_tokens": 10}
        headers = {"Authorization": f"bearer {VALID_API_KEY}"}  # lowercase
        async with session.post(f"{gateway_url}/v1/inference", json=payload, headers=headers) as resp:
            if resp.status == 200:
                return TestResult(name, True)
            else:
                return TestResult(name, False, f"Expected 200, got {resp.status}")
    except Exception as e:
        return TestResult(name, False, str(e))


async def test_metrics_endpoint_no_auth(session: aiohttp.ClientSession, gateway_url: str) -> TestResult:
    """/metrics should not require auth."""
    name = "metrics_no_auth_required"
    try:
        async with session.get(f"{gateway_url}/metrics") as resp:
            if resp.status == 200:
                return TestResult(name, True)
            else:
                return TestResult(name, False, f"Expected 200, got {resp.status}")
    except Exception as e:
        return TestResult(name, False, str(e))


async def test_auth_metrics_recorded(session: aiohttp.ClientSession, gateway_url: str) -> TestResult:
    """Auth success/failure metrics should be recorded."""
    name = "auth_metrics_recorded"
    try:
        # Make a failed auth request
        payload = {"prompt": "test", "priority": 1}
        headers = {"Authorization": f"Bearer {INVALID_API_KEY}"}
        await session.post(f"{gateway_url}/v1/inference", json=payload, headers=headers)
        
        # Check metrics
        async with session.get(f"{gateway_url}/metrics") as resp:
            metrics = await resp.text()
            if "auth_failures_total" in metrics:
                return TestResult(name, True)
            else:
                return TestResult(name, False, "auth_failures_total metric not found")
    except Exception as e:
        return TestResult(name, False, str(e))


async def run_all_tests(gateway_url: str) -> bool:
    """Run all auth tests."""
    print(f"\n{'='*60}")
    print("Auth Integration Tests")
    print(f"Gateway: {gateway_url}")
    print(f"{'='*60}\n")
    
    tests = [
        test_no_auth_header,
        test_invalid_api_key,
        test_malformed_auth_header,
        test_empty_bearer_token,
        test_valid_api_key,
        test_case_insensitive_bearer,
        test_metrics_endpoint_no_auth,
        test_auth_metrics_recorded,
    ]
    
    results = []
    async with aiohttp.ClientSession() as session:
        for test_func in tests:
            result = await test_func(session, gateway_url)
            results.append(result)
    
    passed = 0
    failed = 0
    
    for result in results:
        status = "✓ PASS" if result.passed else "✗ FAIL"
        print(f"{status} | {result.name}")
        if result.error:
            print(f"       Error: {result.error}")
        
        if result.passed:
            passed += 1
        else:
            failed += 1
    
    print(f"\n{'='*60}")
    print(f"Results: {passed} passed, {failed} failed")
    print(f"{'='*60}\n")
    
    return failed == 0


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description="Test auth on inference gateway")
    parser.add_argument("--gateway", type=str, default="http://localhost:8080", help="Gateway URL")
    args = parser.parse_args()
    
    success = asyncio.run(run_all_tests(args.gateway))
    exit(0 if success else 1)
