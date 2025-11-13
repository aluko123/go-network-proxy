# Network Proxy Tests

Organized testing suite for the network proxy server.

## Directory Structure

```
tests/
├── k6/              # k6 load test scripts
├── scripts/         # Python and shell test scripts
├── results/         # Test results (JSON files)
└── README.md        # This file
```

## K6 Load Tests

All k6 tests should be run from the project root directory.

### Basic Tests

**proxy-loadtest.js** - Simple load test
```bash
k6 run tests/k6/proxy-loadtest.js --summary-export=tests/results/basic-test.json
```

### Rate Limiting Tests

**rate-limit-test.js** - Single user, 20 iterations
```bash
k6 run tests/k6/rate-limit-test.js
```

**rate-limit-validation.js** - Aggressive test to trigger rate limits
```bash
k6 run --vus 10 --duration 30s tests/k6/rate-limit-validation.js \
  --summary-export=tests/results/rate-limit-validation.json
```

**rate-limit-multi-user-test.js** - Multiple concurrent users
```bash
k6 run tests/k6/rate-limit-multi-user-test.js \
  --summary-export=tests/results/multi-user-rate-limit.json
```

### Production-Like Tests

**prod-load-test.js** - Realistic traffic patterns
```bash
k6 run tests/k6/prod-load-test.js \
  --summary-export=tests/results/prod-load-test.json
```
- Warm up: 20 users
- Normal load: 50 users  
- Peak: 100 users
- Spike: 200 users
- Duration: ~13 minutes

### Performance Tests

**stress-test.js** - Find breaking point
```bash
k6 run tests/k6/stress-test.js \
  --summary-export=tests/results/stress-test.json
```
- Ramps from 100 → 500 users
- Duration: ~15 minutes
- Identifies maximum capacity

**soak-test.js** - Sustained load for memory leaks
```bash
k6 run tests/k6/soak-test.js \
  --summary-export=tests/results/soak-test.json
```
- 100 users for 1 hour
- Checks for memory leaks and degradation

**spike-test.js** - Sudden traffic burst
```bash
k6 run tests/k6/spike-test.js \
  --summary-export=tests/results/spike-test.json
```
- 50 → 500 users instantly
- Tests recovery from spikes

## Python Tests

**test-rate-limit.py** - Rate limiter validation with Python requests
```bash
python3 tests/scripts/test-rate-limit.py
```

Sends 200 rapid requests to verify rate limiting effectiveness.

## Prerequisites

Before running tests:

1. **Start Python test server:**
   ```bash
   python3 -m http.server 9000 &
   ```

2. **Start proxy server:**
   ```bash
   ./http-proxy/proxy-server
   ```

3. **Optional: Start monitoring:**
   ```bash
   # Terminal 1: Prometheus metrics
   watch -n 1 'curl -s localhost:8080/metrics | grep proxy_'
   
   # Terminal 2: Connection monitoring
   watch -n 1 'ss -tan | grep :8080 | wc -l'
   ```

## Analyzing Results

**View summary:**
```bash
cat tests/results/prod-load-test.json | jq '{
  total_requests: .metrics.http_reqs.count,
  success_rate: .metrics.checks.value,
  p95_latency: .metrics.http_req_duration.p(95),
  rate_limited: .metrics.rate_limit_hits.count
}'
```

**Compare two runs:**
```bash
diff <(jq .metrics.http_req_duration.p(95) tests/results/run1.json) \
     <(jq .metrics.http_req_duration.p(95) tests/results/run2.json)
```

## Success Criteria

### Performance
- ✅ P95 latency < 500ms
- ✅ P99 latency < 1000ms
- ✅ Throughput > 100 req/s

### Reliability
- ✅ Error rate < 1% (excluding rate limits)
- ✅ No 502/503 errors under normal load
- ✅ Graceful degradation under stress

### Rate Limiting
- ✅ Rate limiter blocks > 70% when exceeded
- ✅ Legitimate traffic not blocked
- ✅ Rate limits enforced per IP

## Cleanup

```bash
# Remove all test results
rm tests/results/*.json

# Kill background processes
pkill -f "python3 -m http.server"
pkill proxy-server
```
