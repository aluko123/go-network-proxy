#!/bin/bash

# Inference Gateway Test Suite
# Starts workers + gateway, runs all tests, cleans up

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
RESULTS_DIR="$SCRIPT_DIR/results"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# PIDs for cleanup
WORKER_PIDS=()
GATEWAY_PID=""
MOCK_API_PID=""

cleanup() {
    echo -e "\n${YELLOW}Cleaning up...${NC}"
    
    # Kill workers
    for pid in "${WORKER_PIDS[@]}"; do
        if kill -0 "$pid" 2>/dev/null; then
            kill "$pid" 2>/dev/null || true
        fi
    done
    
    # Kill gateway
    if [ -n "$GATEWAY_PID" ] && kill -0 "$GATEWAY_PID" 2>/dev/null; then
        kill "$GATEWAY_PID" 2>/dev/null || true
    fi
    
    # Kill mock API server
    if [ -n "$MOCK_API_PID" ] && kill -0 "$MOCK_API_PID" 2>/dev/null; then
        kill "$MOCK_API_PID" 2>/dev/null || true
    fi
    
    echo -e "${GREEN}Cleanup complete${NC}"
}

trap cleanup EXIT

wait_for_service() {
    local url=$1
    local name=$2
    local max_attempts=30
    local attempt=0
    
    echo -n "Waiting for $name..."
    while [ $attempt -lt $max_attempts ]; do
        if curl -s "$url" > /dev/null 2>&1; then
            echo -e " ${GREEN}ready${NC}"
            return 0
        fi
        sleep 0.5
        attempt=$((attempt + 1))
        echo -n "."
    done
    
    echo -e " ${RED}failed${NC}"
    return 1
}

echo -e "\n${GREEN}======================================${NC}"
echo -e "${GREEN}  Inference Gateway Test Suite${NC}"
echo -e "${GREEN}======================================${NC}\n"

cd "$PROJECT_ROOT"

# Create results directory
mkdir -p "$RESULTS_DIR"

# --- 1. Build Gateway ---
echo -e "${YELLOW}Building gateway...${NC}"
go build -o gateway cmd/gateway/main.go
echo -e "${GREEN}Build complete${NC}\n"

# --- 2. Start Mock API Server (OpenAI/Anthropic) ---
echo -e "${YELLOW}Starting mock API server...${NC}"
python3 workers/mock_api_server.py --port 8000 --latency 0.01 &
MOCK_API_PID=$!
echo "  Mock API Server (PID $MOCK_API_PID) on :8000"

# --- 3. Start Mock gRPC Workers ---
echo -e "${YELLOW}Starting mock gRPC workers...${NC}"

python3 workers/mock_server.py --port 50051 --latency 0.01 --model "worker-1" &
WORKER_PIDS+=($!)
echo "  Worker 1 (PID ${WORKER_PIDS[-1]}) on :50051"

python3 workers/mock_server.py --port 50052 --latency 0.01 --model "worker-2" &
WORKER_PIDS+=($!)
echo "  Worker 2 (PID ${WORKER_PIDS[-1]}) on :50052"

sleep 2

# --- 4. Start Gateway ---
echo -e "\n${YELLOW}Starting gateway...${NC}"
./gateway \
    -backends configs/backends-local.yaml \
    -limiter memory \
    -rate-limit 100 \
    -rate-burst 20 \
    -log-format text \
    > "$RESULTS_DIR/gateway.log" 2>&1 &
GATEWAY_PID=$!
echo "  Gateway (PID $GATEWAY_PID) on :8080"

# --- 4. Wait for Services ---
echo ""
wait_for_service "http://localhost:8080/metrics" "gateway" || exit 1

# --- 5. Run Test Suites ---
echo -e "\n${GREEN}======================================${NC}"
echo -e "${GREEN}  Running Tests${NC}"
echo -e "${GREEN}======================================${NC}\n"

TESTS_PASSED=0
TESTS_FAILED=0

# 5a. Unit Tests
echo -e "${YELLOW}[1/4] Unit Tests${NC}"
if go test ./... -v > "$RESULTS_DIR/unit-tests.log" 2>&1; then
    echo -e "${GREEN}  ✓ Unit tests passed${NC}"
    TESTS_PASSED=$((TESTS_PASSED + 1))
else
    echo -e "${RED}  ✗ Unit tests failed${NC}"
    echo "     See: $RESULTS_DIR/unit-tests.log"
    TESTS_FAILED=$((TESTS_FAILED + 1))
fi

# 5b. Auth Tests
echo -e "\n${YELLOW}[2/4] Auth Tests${NC}"
if python3 tests/scripts/test-auth.py --gateway http://localhost:8080 > "$RESULTS_DIR/auth-tests.log" 2>&1; then
    echo -e "${GREEN}  ✓ Auth tests passed${NC}"
    TESTS_PASSED=$((TESTS_PASSED + 1))
else
    echo -e "${RED}  ✗ Auth tests failed${NC}"
    echo "     See: $RESULTS_DIR/auth-tests.log"
    TESTS_FAILED=$((TESTS_FAILED + 1))
fi

# 5c. Integration Tests
echo -e "\n${YELLOW}[3/4] Integration Tests${NC}"
if python3 tests/scripts/test-inference-gateway.py --gateway http://localhost:8080 > "$RESULTS_DIR/integration-tests.log" 2>&1; then
    echo -e "${GREEN}  ✓ Integration tests passed${NC}"
    TESTS_PASSED=$((TESTS_PASSED + 1))
else
    echo -e "${RED}  ✗ Integration tests failed${NC}"
    echo "     See: $RESULTS_DIR/integration-tests.log"
    TESTS_FAILED=$((TESTS_FAILED + 1))
fi

# 5d. Load Tests (light)
echo -e "\n${YELLOW}[4/4] Load Tests (light)${NC}"
if python3 tests/scripts/load-test-inference.py \
    --gateway http://localhost:8080 \
    --rps 20 \
    --duration 10 \
    > "$RESULTS_DIR/load-tests.log" 2>&1; then
    echo -e "${GREEN}  ✓ Load tests passed${NC}"
    TESTS_PASSED=$((TESTS_PASSED + 1))
else
    echo -e "${RED}  ✗ Load tests failed${NC}"
    echo "     See: $RESULTS_DIR/load-tests.log"
    TESTS_FAILED=$((TESTS_FAILED + 1))
fi

# --- 6. Summary ---
echo -e "\n${GREEN}======================================${NC}"
echo -e "${GREEN}  Results${NC}"
echo -e "${GREEN}======================================${NC}\n"

echo "  Passed: $TESTS_PASSED"
echo "  Failed: $TESTS_FAILED"
echo ""
echo "  Logs: $RESULTS_DIR/"
echo ""

if [ $TESTS_FAILED -gt 0 ]; then
    echo -e "${RED}Some tests failed!${NC}"
    exit 1
else
    echo -e "${GREEN}All tests passed!${NC}"
    exit 0
fi
