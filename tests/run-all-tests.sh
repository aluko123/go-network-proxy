#!/bin/bash

# Network Proxy Test Suite Runner
# Run all k6 tests and save results

set -e

RESULTS_DIR="tests/results"
K6_DIR="tests/k6"

echo "🚀 Starting Network Proxy Test Suite"
echo "======================================"
echo ""

# Check prerequisites
echo "Checking prerequisites..."
if ! command -v k6 &> /dev/null; then
    echo "❌ k6 is not installed"
    exit 1
fi

if ! curl -s http://localhost:8080/metrics > /dev/null; then
    echo "❌ Proxy server is not running on port 8080"
    echo "   Start it with: ./http-proxy/proxy-server"
    exit 1
fi

if ! curl -s http://localhost:9000 > /dev/null; then
    echo "⚠️  Python test server not running on port 9000"
    echo "   Starting it now..."
    python3 -m http.server 9000 &
    sleep 2
fi

echo "✅ All prerequisites met"
echo ""

# Create results directory
mkdir -p $RESULTS_DIR

# Run tests
echo "Running test suite..."
echo ""

echo "📊 Test 1/5: Basic Load Test"
k6 run $K6_DIR/proxy-loadtest.js \
  --summary-export=$RESULTS_DIR/basic-load-test.json \
  --quiet

echo "📊 Test 2/5: Rate Limit Validation"
k6 run --vus 10 --duration 10s $K6_DIR/rate-limit-validation.js \
  --summary-export=$RESULTS_DIR/rate-limit-validation.json \
  --quiet

echo "📊 Test 3/5: Production Load Test"
k6 run $K6_DIR/prod-load-test.js \
  --summary-export=$RESULTS_DIR/prod-load-test.json \
  --quiet

echo "📊 Test 4/5: Spike Test"
k6 run $K6_DIR/spike-test.js \
  --summary-export=$RESULTS_DIR/spike-test.json \
  --quiet

echo "📊 Test 5/5: Python Rate Limit Test"
python3 tests/scripts/test-rate-limit.py > $RESULTS_DIR/python-rate-limit.txt

echo ""
echo "✅ All tests completed!"
echo ""
echo "Results saved to: $RESULTS_DIR/"
echo ""

# Summary
echo "📈 Quick Summary:"
echo "================"
echo ""
echo "Basic Load Test:"
jq -r '.metrics.http_reqs.count as $total | .metrics.http_req_duration.p(95) as $p95 | "  Requests: \($total), P95: \($p95)ms"' \
  $RESULTS_DIR/basic-load-test.json

echo ""
echo "Rate Limit Validation:"
jq -r '.metrics.rate_limited.count as $blocked | .metrics.allowed.count as $allowed | "  Blocked: \($blocked), Allowed: \($allowed)"' \
  $RESULTS_DIR/rate-limit-validation.json

echo ""
echo "Production Load Test:"
jq -r '.metrics.http_reqs.count as $total | .metrics.rate_limit_hits.count as $blocked | .metrics.http_req_duration.p(95) as $p95 | "  Requests: \($total), Rate Limited: \($blocked), P95: \($p95)ms"' \
  $RESULTS_DIR/prod-load-test.json

echo ""
echo "View full results: ls -lh $RESULTS_DIR/"
