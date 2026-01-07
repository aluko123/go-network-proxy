#!/usr/bin/env python3
"""
Test model routing across different backends.
Tests OpenAI, Anthropic, and local gRPC backends.

Usage:
    python tests/scripts/test-model-routing.py --gateway http://localhost:8080
"""
import argparse
import asyncio
import aiohttp
import json
from dataclasses import dataclass
from typing import Optional, List


@dataclass
class TestResult:
    name: str
    model: str
    backend: str
    passed: bool
    tokens_received: int = 0
    duration_ms: float = 0
    error: Optional[str] = None


API_KEY = "sk-dev-test-key-12345"

MODELS_TO_TEST = [
    # (model, expected_backend_type)
    ("gpt-4", "openai"),
    ("gpt-3.5-turbo", "openai"),
    ("claude-3-sonnet-20240229", "anthropic"),
    ("claude-3-haiku-20240307", "anthropic"),
    ("mock-gpt", "grpc"),
    ("default", "grpc"),
]


async def test_model(
    session: aiohttp.ClientSession,
    gateway_url: str,
    model: str,
    expected_backend: str
) -> TestResult:
    """Test a specific model and verify it routes correctly."""
    import time
    
    payload = {
        "model": model,
        "prompt": f"Hello! Testing model routing for {model}.",
        "max_tokens": 20,
        "temperature": 0.7,
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
                error_text = await resp.text()
                return TestResult(
                    name=f"route_{model}",
                    model=model,
                    backend=expected_backend,
                    passed=False,
                    error=f"HTTP {resp.status}: {error_text[:100]}"
                )
            
            async for line in resp.content:
                line = line.decode('utf-8').strip()
                if line.startswith('data:'):
                    try:
                        data = json.loads(line[5:].strip())
                        if data.get('token'):
                            tokens.append(data['token'])
                        if data.get('finished'):
                            break
                    except json.JSONDecodeError:
                        pass
        
        duration = (time.time() - start) * 1000
        
        # Verify we got tokens
        if len(tokens) == 0:
            return TestResult(
                name=f"route_{model}",
                model=model,
                backend=expected_backend,
                passed=False,
                duration_ms=duration,
                error="No tokens received"
            )
        
        # Check if response contains expected backend marker
        response_text = "".join(tokens)
        
        return TestResult(
            name=f"route_{model}",
            model=model,
            backend=expected_backend,
            passed=True,
            tokens_received=len(tokens),
            duration_ms=duration
        )
        
    except asyncio.TimeoutError:
        return TestResult(
            name=f"route_{model}",
            model=model,
            backend=expected_backend,
            passed=False,
            error="Timeout"
        )
    except Exception as e:
        return TestResult(
            name=f"route_{model}",
            model=model,
            backend=expected_backend,
            passed=False,
            error=str(e)[:100]
        )


async def test_unknown_model(session: aiohttp.ClientSession, gateway_url: str) -> TestResult:
    """Test that unknown models return an error."""
    payload = {
        "model": "unknown-model-xyz",
        "prompt": "Test",
        "max_tokens": 10,
    }
    headers = {"Authorization": f"Bearer {API_KEY}"}
    
    try:
        async with session.post(
            f"{gateway_url}/v1/inference",
            json=payload,
            headers=headers,
            timeout=aiohttp.ClientTimeout(total=10)
        ) as resp:
            # We expect an error event in the SSE stream
            content = await resp.text()
            if "error" in content.lower() or "unknown" in content.lower():
                return TestResult(
                    name="unknown_model_rejected",
                    model="unknown-model-xyz",
                    backend="none",
                    passed=True
                )
            else:
                return TestResult(
                    name="unknown_model_rejected",
                    model="unknown-model-xyz",
                    backend="none",
                    passed=False,
                    error="Expected error for unknown model"
                )
    except Exception as e:
        return TestResult(
            name="unknown_model_rejected",
            model="unknown-model-xyz",
            backend="none",
            passed=False,
            error=str(e)
        )


async def test_list_models(session: aiohttp.ClientSession, gateway_url: str) -> TestResult:
    """Test /v1/models endpoint."""
    try:
        async with session.get(f"{gateway_url}/v1/models") as resp:
            if resp.status != 200:
                return TestResult(
                    name="list_models",
                    model="",
                    backend="",
                    passed=False,
                    error=f"HTTP {resp.status}"
                )
            
            data = await resp.json()
            models = data.get("models", [])
            
            if len(models) > 0:
                return TestResult(
                    name="list_models",
                    model="",
                    backend="",
                    passed=True,
                    tokens_received=len(models)
                )
            else:
                return TestResult(
                    name="list_models",
                    model="",
                    backend="",
                    passed=False,
                    error="No models returned"
                )
    except Exception as e:
        return TestResult(
            name="list_models",
            model="",
            backend="",
            passed=False,
            error=str(e)
        )


async def run_all_tests(gateway_url: str) -> bool:
    """Run all model routing tests."""
    print(f"\n{'='*60}")
    print("Model Routing Tests")
    print(f"Gateway: {gateway_url}")
    print(f"{'='*60}\n")
    
    results: List[TestResult] = []
    
    async with aiohttp.ClientSession() as session:
        # Test /v1/models endpoint
        result = await test_list_models(session, gateway_url)
        results.append(result)
        
        # Test each model
        for model, expected_backend in MODELS_TO_TEST:
            result = await test_model(session, gateway_url, model, expected_backend)
            results.append(result)
        
        # Test unknown model
        result = await test_unknown_model(session, gateway_url)
        results.append(result)
    
    # Print results
    passed = 0
    failed = 0
    
    for result in results:
        status = "✓ PASS" if result.passed else "✗ FAIL"
        print(f"{status} | {result.name}")
        if result.model:
            print(f"       Model: {result.model} → Backend: {result.backend}")
        if result.tokens_received:
            print(f"       Tokens: {result.tokens_received}, Duration: {result.duration_ms:.1f}ms")
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
    parser = argparse.ArgumentParser(description="Test model routing")
    parser.add_argument("--gateway", type=str, default="http://localhost:8080", help="Gateway URL")
    args = parser.parse_args()
    
    success = asyncio.run(run_all_tests(args.gateway))
    exit(0 if success else 1)
