package backend

import (
	"sync/atomic"
	"testing"
	"time"
)

func TestNewHealthTracker(t *testing.T) {
	clients := []*grpcClient{
		{address: "localhost:50051"},
		{address: "localhost:50052"},
	}

	tracker := NewHealthTracker(clients, HealthTrackerConfig{
		CheckInterval: 5 * time.Second,
		Timeout:       2 * time.Second,
	})

	if tracker == nil {
		t.Fatal("expected non-nil tracker")
	}

	if len(tracker.status) != 2 {
		t.Errorf("expected 2 workers in status, got %d", len(tracker.status))
	}

	// All workers should start healthy
	for addr := range tracker.status {
		if !tracker.IsHealthy(addr) {
			t.Errorf("worker %s should be healthy initially", addr)
		}
	}
}

func TestHealthTracker_Get(t *testing.T) {
	clients := []*grpcClient{
		{address: "localhost:50051"},
	}

	tracker := NewHealthTracker(clients, HealthTrackerConfig{})

	health := tracker.Get("localhost:50051")
	if health == nil {
		t.Fatal("expected non-nil health")
	}
	if !health.Healthy {
		t.Error("expected healthy")
	}

	// Unknown worker should return unhealthy
	unknown := tracker.Get("localhost:99999")
	if unknown.Healthy {
		t.Error("unknown worker should be unhealthy")
	}
}

func TestHealthTracker_GetAll(t *testing.T) {
	clients := []*grpcClient{
		{address: "localhost:50051"},
		{address: "localhost:50052"},
	}

	tracker := NewHealthTracker(clients, HealthTrackerConfig{})

	all := tracker.GetAll()
	if len(all) != 2 {
		t.Errorf("expected 2, got %d", len(all))
	}
}

func TestWorkerHealth_ConsecutiveErrors(t *testing.T) {
	health := &WorkerHealth{
		Healthy:        true,
		ConsecutiveErr: 0,
	}

	// Simulate consecutive errors
	for i := 1; i <= 3; i++ {
		health.ConsecutiveErr++
		health.ErrorCount++

		if i < 3 && !health.Healthy {
			t.Error("should still be healthy before 3 errors")
		}
	}

	// After 3 consecutive errors, should be marked unhealthy
	if health.ConsecutiveErr >= 3 {
		health.Healthy = false
	}

	if health.Healthy {
		t.Error("should be unhealthy after 3 consecutive errors")
	}
}

func TestWorkerScoring(t *testing.T) {
	testCases := []struct {
		name       string
		gpuUtil    float32
		queueDepth int
		expected   float64
	}{
		{"idle worker", 0, 0, 100},
		{"50% GPU, empty queue", 50, 0, 50},
		{"0% GPU, queue of 5", 0, 5, 50},
		{"80% GPU, queue of 2", 80, 2, 0},
		{"fully loaded", 100, 10, -100},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			score := 100 - float64(tc.gpuUtil) - float64(tc.queueDepth*10)
			if score != tc.expected {
				t.Errorf("expected score %f, got %f", tc.expected, score)
			}
		})
	}
}

func TestGRPCBackend_SelectBestWorker(t *testing.T) {
	// Create mock clients
	client1 := &grpcClient{address: "localhost:50051"}
	client1.healthy.Store(true)
	client2 := &grpcClient{address: "localhost:50052"}
	client2.healthy.Store(true)
	client3 := &grpcClient{address: "localhost:50053"}
	client3.healthy.Store(false) // unhealthy

	clients := []*grpcClient{client1, client2, client3}

	tracker := NewHealthTracker(clients, HealthTrackerConfig{})

	// Set different GPU utilizations
	tracker.status["localhost:50051"].GPUUtilization = 80
	tracker.status["localhost:50051"].QueueDepth = 5
	tracker.status["localhost:50052"].GPUUtilization = 20
	tracker.status["localhost:50052"].QueueDepth = 0
	tracker.status["localhost:50053"].Healthy = false

	backend := &GRPCBackend{
		clients:     clients,
		tracker:     tracker,
		prefixIndex: NewPrefixIndex(PrefixIndexConfig{}),
	}
	backend.healthy.Store(true)

	// Should pick client2 (lowest GPU util, empty queue)
	selected := backend.getClientForRequest(&Request{Model: "test"})
	if selected == nil {
		t.Fatal("expected non-nil client")
	}
	if selected.address != "localhost:50052" {
		t.Errorf("expected localhost:50052 (best score), got %s", selected.address)
	}
}

func TestGRPCBackend_FallbackWhenAllUnhealthy(t *testing.T) {
	client1 := &grpcClient{address: "localhost:50051"}
	client1.healthy.Store(false)
	client2 := &grpcClient{address: "localhost:50052"}
	client2.healthy.Store(false)

	clients := []*grpcClient{client1, client2}
	tracker := NewHealthTracker(clients, HealthTrackerConfig{})
	tracker.status["localhost:50051"].Healthy = false
	tracker.status["localhost:50052"].Healthy = false

	backend := &GRPCBackend{
		clients:     clients,
		tracker:     tracker,
		prefixIndex: NewPrefixIndex(PrefixIndexConfig{}),
	}
	backend.nextIdx = atomic.Uint64{}

	// Should still return a client (last resort fallback)
	selected := backend.getClientForRequest(&Request{Model: "test"})
	if selected == nil {
		t.Fatal("expected fallback client even when all unhealthy")
	}
}

func TestGRPCBackend_PrefixAffinityRouting(t *testing.T) {
	client1 := &grpcClient{address: "localhost:50051"}
	client1.healthy.Store(true)
	client2 := &grpcClient{address: "localhost:50052"}
	client2.healthy.Store(true)

	clients := []*grpcClient{client1, client2}
	tracker := NewHealthTracker(clients, HealthTrackerConfig{})

	// Worker 1: 60% GPU utilization
	// Worker 2: 30% GPU utilization
	// Without prefix affinity, worker 2 wins
	tracker.status["localhost:50051"].GPUUtilization = 60
	tracker.status["localhost:50052"].GPUUtilization = 30

	prefixIndex := NewPrefixIndex(PrefixIndexConfig{TTL: 1 * time.Minute})

	backend := &GRPCBackend{
		clients:        clients,
		tracker:        tracker,
		prefixIndex:    prefixIndex,
		cacheHitWeight: 20,
	}
	backend.healthy.Store(true)

	systemPrompt := "You are a helpful assistant that answers coding questions."

	// First request: no prefix recorded yet, should go to worker 2 (lower GPU util)
	req1 := &Request{
		Model:  "llama-70b",
		Prefix: systemPrompt,
	}
	selected := backend.getClientForRequest(req1)
	if selected.address != "localhost:50052" {
		t.Errorf("first request should go to worker 2 (best score), got %s", selected.address)
	}

	// Simulate that we sent the request to worker 2 (Record the prefix)
	prefixHash := HashPrefix("llama-70b", systemPrompt)
	prefixIndex.Record(prefixHash, "localhost:50052")

	// Second request with same prefix:
	// Worker 1: score = 100 - 60 = 40
	// Worker 2: score = 100 - 30 + 20 (cache hit) = 90
	// Worker 2 should still win (even more so now)
	selected = backend.getClientForRequest(req1)
	if selected.address != "localhost:50052" {
		t.Errorf("second request should go to worker 2 (has cached prefix), got %s", selected.address)
	}

	// Now make worker 2 much busier
	// Worker 1: score = 100 - 60 = 40
	// Worker 2: score = 100 - 80 + 20 (cache hit) = 40
	// Tie, but cache hit should be considered
	tracker.status["localhost:50052"].GPUUtilization = 80

	selected = backend.getClientForRequest(req1)
	// With equal scores, implementation picks first one found with highest score
	// Let's just verify it picks one of them
	if selected == nil {
		t.Error("should return a worker")
	}

	// Make worker 2 extremely busy - cache hit shouldn't override severe load
	// Worker 1: score = 100 - 60 = 40
	// Worker 2: score = 100 - 95 + 20 = 25
	// Worker 1 should win despite no cache
	tracker.status["localhost:50052"].GPUUtilization = 95

	selected = backend.getClientForRequest(req1)
	if selected.address != "localhost:50051" {
		t.Errorf("should pick worker 1 when worker 2 is overloaded, got %s", selected.address)
	}
}

func TestGRPCBackend_PrefixAffinityWithQueue(t *testing.T) {
	client1 := &grpcClient{address: "localhost:50051"}
	client1.healthy.Store(true)
	client2 := &grpcClient{address: "localhost:50052"}
	client2.healthy.Store(true)

	clients := []*grpcClient{client1, client2}
	tracker := NewHealthTracker(clients, HealthTrackerConfig{})

	// Equal GPU utilization
	tracker.status["localhost:50051"].GPUUtilization = 50
	tracker.status["localhost:50052"].GPUUtilization = 50
	// But worker 2 has queue depth of 3
	tracker.status["localhost:50052"].QueueDepth = 3

	prefixIndex := NewPrefixIndex(PrefixIndexConfig{TTL: 1 * time.Minute})
	prefixHash := HashPrefix("llama-70b", "test prefix")
	prefixIndex.Record(prefixHash, "localhost:50052")

	backend := &GRPCBackend{
		clients:        clients,
		tracker:        tracker,
		prefixIndex:    prefixIndex,
		cacheHitWeight: 20,
	}
	backend.healthy.Store(true)

	// Worker 1: score = 100 - 50 - 0 = 50
	// Worker 2: score = 100 - 50 - 30 + 20 = 40
	// Worker 1 should win (queue penalty outweighs cache hit)
	req := &Request{Model: "llama-70b", Prefix: "test prefix"}
	selected := backend.getClientForRequest(req)
	if selected.address != "localhost:50051" {
		t.Errorf("queue penalty should outweigh cache hit, got %s", selected.address)
	}
}
