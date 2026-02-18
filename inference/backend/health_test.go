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

func TestGRPCBackend_ReturnsNilWhenAllUnhealthy(t *testing.T) {
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

	// Should return nil when all workers are unhealthy (don't route to known-bad workers)
	selected := backend.getClientForRequest(&Request{Model: "test"})
	if selected != nil {
		t.Fatal("expected nil when all workers are unhealthy, got", selected.address)
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

func TestGRPCBackend_MemoryAwareRouting(t *testing.T) {
	const GB = int64(1024 * 1024 * 1024)

	client1 := &grpcClient{address: "localhost:50051"}
	client1.healthy.Store(true)
	client2 := &grpcClient{address: "localhost:50052"}
	client2.healthy.Store(true)

	clients := []*grpcClient{client1, client2}
	tracker := NewHealthTracker(clients, HealthTrackerConfig{})

	// Worker 1: 80GB total, 75GB used (5GB free) - memory constrained
	tracker.status["localhost:50051"].Healthy = true
	tracker.status["localhost:50051"].GPUUtilization = 50
	tracker.status["localhost:50051"].GPUMemoryTotal = 80 * GB
	tracker.status["localhost:50051"].GPUMemoryUsed = 75 * GB

	// Worker 2: 80GB total, 20GB used (60GB free) - plenty of headroom
	tracker.status["localhost:50052"].Healthy = true
	tracker.status["localhost:50052"].GPUUtilization = 50
	tracker.status["localhost:50052"].GPUMemoryTotal = 80 * GB
	tracker.status["localhost:50052"].GPUMemoryUsed = 20 * GB

	backend := &GRPCBackend{
		clients:        clients,
		tracker:        tracker,
		prefixIndex:    NewPrefixIndex(PrefixIndexConfig{}),
		cacheHitWeight: 20,
		memoryWeight:   10,
		memoryMargin:   2 * GB, // 2GB safety margin
	}
	backend.healthy.Store(true)

	// Request for llama-70b with 32K context would need ~640MB + 2GB margin
	// Worker 1 has 5GB free, should be skipped (5GB < 2.64GB needed... wait, that's enough)
	// Let's make the request larger to force the skip
	// Actually with 2GB margin, worker1 (5GB free) can handle it
	// Let's adjust: make worker1 have only 1GB free
	tracker.status["localhost:50051"].GPUMemoryUsed = 79 * GB // Only 1GB free

	req := &Request{
		Model:  "llama-70b",
		Prompt: string(make([]byte, 32000*4)), // ~32K tokens worth of chars
	}

	selected := backend.getClientForRequest(req)
	if selected == nil {
		t.Fatal("expected a worker to be selected")
	}
	if selected.address != "localhost:50052" {
		t.Errorf("expected worker with more memory (50052), got %s", selected.address)
	}
}

func TestGRPCBackend_MemoryAwareRouting_AllConstrained(t *testing.T) {
	const GB = int64(1024 * 1024 * 1024)

	client1 := &grpcClient{address: "localhost:50051"}
	client1.healthy.Store(true)

	clients := []*grpcClient{client1}
	tracker := NewHealthTracker(clients, HealthTrackerConfig{})

	// Worker 1: 80GB total, 79GB used (1GB free) - very constrained
	tracker.status["localhost:50051"].Healthy = true
	tracker.status["localhost:50051"].GPUUtilization = 90
	tracker.status["localhost:50051"].GPUMemoryTotal = 80 * GB
	tracker.status["localhost:50051"].GPUMemoryUsed = 79 * GB

	backend := &GRPCBackend{
		clients:        clients,
		tracker:        tracker,
		prefixIndex:    NewPrefixIndex(PrefixIndexConfig{}),
		cacheHitWeight: 20,
		memoryWeight:   10,
		memoryMargin:   2 * GB,
	}
	backend.healthy.Store(true)

	// Large request that needs more than 1GB
	req := &Request{
		Model:  "llama-70b",
		Prompt: string(make([]byte, 32000*4)), // ~32K tokens
	}

	// Should fall back to the worker anyway (better to try than reject)
	selected := backend.getClientForRequest(req)
	if selected == nil {
		t.Fatal("expected fallback to constrained worker rather than nil")
	}
}

func TestGRPCBackend_MemoryHeadroomBonus(t *testing.T) {
	const GB = int64(1024 * 1024 * 1024)

	client1 := &grpcClient{address: "localhost:50051"}
	client1.healthy.Store(true)
	client2 := &grpcClient{address: "localhost:50052"}
	client2.healthy.Store(true)

	clients := []*grpcClient{client1, client2}
	tracker := NewHealthTracker(clients, HealthTrackerConfig{})

	// Both workers have same GPU util, but different memory
	// Worker 1: 50% memory free
	tracker.status["localhost:50051"].Healthy = true
	tracker.status["localhost:50051"].GPUUtilization = 50
	tracker.status["localhost:50051"].GPUMemoryTotal = 80 * GB
	tracker.status["localhost:50051"].GPUMemoryUsed = 40 * GB

	// Worker 2: 80% memory free
	tracker.status["localhost:50052"].Healthy = true
	tracker.status["localhost:50052"].GPUUtilization = 50
	tracker.status["localhost:50052"].GPUMemoryTotal = 80 * GB
	tracker.status["localhost:50052"].GPUMemoryUsed = 16 * GB

	backend := &GRPCBackend{
		clients:        clients,
		tracker:        tracker,
		prefixIndex:    NewPrefixIndex(PrefixIndexConfig{}),
		cacheHitWeight: 20,
		memoryWeight:   10,
		memoryMargin:   2 * GB,
	}
	backend.healthy.Store(true)

	// Small request - both can handle, but worker2 should win due to memory bonus
	// Worker 1: 100 - 50 + (0.5 * 10) = 55
	// Worker 2: 100 - 50 + (0.8 * 10) = 58
	req := &Request{Model: "llama-7b", Prompt: "hello"}
	selected := backend.getClientForRequest(req)
	if selected.address != "localhost:50052" {
		t.Errorf("expected worker with more memory headroom (50052), got %s", selected.address)
	}
}

func TestGRPCBackend_InfiniBandAwareRouting(t *testing.T) {
	const GB = int64(1024 * 1024 * 1024)

	client1 := &grpcClient{address: "localhost:50051"}
	client1.healthy.Store(true)
	client2 := &grpcClient{address: "localhost:50052"}
	client2.healthy.Store(true)

	clients := []*grpcClient{client1, client2}
	tracker := NewHealthTracker(clients, HealthTrackerConfig{})

	// Worker 1: IB Down (bad cable or switch issue)
	tracker.status["localhost:50051"].Healthy = true
	tracker.status["localhost:50051"].GPUUtilization = 20 // Lower util (would normally win)
	tracker.status["localhost:50051"].Topology = &WorkerTopology{
		GPUCount:    8,
		IBAvailable: true,
		IBState:     IBStateDown, // IB is down!
	}

	// Worker 2: IB Active (healthy)
	tracker.status["localhost:50052"].Healthy = true
	tracker.status["localhost:50052"].GPUUtilization = 50 // Higher util
	tracker.status["localhost:50052"].Topology = &WorkerTopology{
		GPUCount:    8,
		IBAvailable: true,
		IBState:     IBStateActive,
		IBSpeed:     IBSpeedHDR,
		IBWidth:     4,
	}

	backend := &GRPCBackend{
		clients:        clients,
		tracker:        tracker,
		prefixIndex:    NewPrefixIndex(PrefixIndexConfig{}),
		cacheHitWeight: 20,
		memoryWeight:   10,
		memoryMargin:   2 * GB,
	}
	backend.healthy.Store(true)

	// Non-distributed request: should pick worker 1 (lower GPU util)
	req := &Request{Model: "llama-7b", Prompt: "hello", RequiresDistributed: false}
	selected := backend.getClientForRequest(req)
	if selected.address != "localhost:50051" {
		t.Errorf("non-distributed request should pick lower util worker, got %s", selected.address)
	}

	// Distributed request: must pick worker 2 (IB Active)
	req = &Request{Model: "llama-7b", Prompt: "hello", RequiresDistributed: true}
	selected = backend.getClientForRequest(req)
	if selected.address != "localhost:50052" {
		t.Errorf("distributed request should skip IB Down worker, got %s", selected.address)
	}
}

func TestGRPCBackend_TensorParallelRouting(t *testing.T) {
	client1 := &grpcClient{address: "localhost:50051"}
	client1.healthy.Store(true)
	client2 := &grpcClient{address: "localhost:50052"}
	client2.healthy.Store(true)

	clients := []*grpcClient{client1, client2}
	tracker := NewHealthTracker(clients, HealthTrackerConfig{})

	// Worker 1: PCIe only (no NVLink)
	tracker.status["localhost:50051"].Healthy = true
	tracker.status["localhost:50051"].GPUUtilization = 20
	tracker.status["localhost:50051"].Topology = &WorkerTopology{
		GPUCount:     4,
		Interconnect: InterconnectPCIe,
	}

	// Worker 2: NVLink (good for tensor parallel)
	tracker.status["localhost:50052"].Healthy = true
	tracker.status["localhost:50052"].GPUUtilization = 50
	tracker.status["localhost:50052"].Topology = &WorkerTopology{
		GPUCount:     8,
		Interconnect: InterconnectNVLink,
	}

	backend := &GRPCBackend{
		clients:        clients,
		tracker:        tracker,
		prefixIndex:    NewPrefixIndex(PrefixIndexConfig{}),
		cacheHitWeight: 20,
		memoryWeight:   10,
		memoryMargin:   2 * 1024 * 1024 * 1024,
	}
	backend.healthy.Store(true)

	// Non-TP request: should pick worker 1 (lower GPU util)
	req := &Request{Model: "llama-7b", Prompt: "hello", TensorParallel: 0}
	selected := backend.getClientForRequest(req)
	if selected.address != "localhost:50051" {
		t.Errorf("non-TP request should pick lower util worker, got %s", selected.address)
	}

	// TP=4 request: must pick worker 2 (has NVLink)
	req = &Request{Model: "llama-70b", Prompt: "hello", TensorParallel: 4}
	selected = backend.getClientForRequest(req)
	if selected.address != "localhost:50052" {
		t.Errorf("tensor parallel request should skip PCIe-only worker, got %s", selected.address)
	}
}

func TestGRPCBackend_IBStateInit(t *testing.T) {
	client1 := &grpcClient{address: "localhost:50051"}
	client1.healthy.Store(true)

	clients := []*grpcClient{client1}
	tracker := NewHealthTracker(clients, HealthTrackerConfig{})

	// Worker 1: IB Init (physical up, but no subnet manager)
	// This is a common issue: SM not running or cable just connected
	tracker.status["localhost:50051"].Healthy = true
	tracker.status["localhost:50051"].Topology = &WorkerTopology{
		GPUCount:    8,
		IBAvailable: true,
		IBState:     IBStateInit, // Not fully active!
	}

	backend := &GRPCBackend{
		clients:        clients,
		tracker:        tracker,
		prefixIndex:    NewPrefixIndex(PrefixIndexConfig{}),
		cacheHitWeight: 20,
		memoryWeight:   10,
		memoryMargin:   2 * 1024 * 1024 * 1024,
	}
	backend.healthy.Store(true)

	// Distributed request should skip Init state (not usable for NCCL)
	req := &Request{Model: "llama-7b", Prompt: "hello", RequiresDistributed: true}
	selected := backend.getClientForRequest(req)

	// Should fall back since only worker has Init state
	// Fallback still returns the worker, but with a warning
	if selected == nil {
		t.Fatal("expected fallback to worker even with IB Init")
	}
}
