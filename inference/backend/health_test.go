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
		clients: clients,
		tracker: tracker,
	}
	backend.healthy.Store(true)

	// Should pick client2 (lowest GPU util, empty queue)
	selected := backend.getClient()
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
		clients: clients,
		tracker: tracker,
	}
	backend.nextIdx = atomic.Uint64{}

	// Should still return a client (last resort fallback)
	selected := backend.getClient()
	if selected == nil {
		t.Fatal("expected fallback client even when all unhealthy")
	}
}
