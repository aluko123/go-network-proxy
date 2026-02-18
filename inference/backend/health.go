package backend

import (
	"context"
	"log/slog"
	"sync"
	"time"

	pb "github.com/aluko123/go-network-proxy/inference/pb"
	"github.com/aluko123/go-network-proxy/pkg/metrics"
)

type HealthTracker struct {
	mu            sync.RWMutex
	status        map[string]*WorkerHealth
	clients       []*grpcClient
	checkInterval time.Duration
	timeout       time.Duration
}

type WorkerHealth struct {
	Healthy        bool
	GPUUtilization float32
	QueueDepth     int
	LastCheck      time.Time
	ErrorCount     int
	ConsecutiveErr int
	AvgLatencyMs   float64

	// GPU memory for memory-aware routing
	GPUMemoryUsed  int64 // Bytes currently in use
	GPUMemoryTotal int64 // Total bytes available

	// Topology for multi-node awareness
	Topology *WorkerTopology
}

type HealthTrackerConfig struct {
	CheckInterval time.Duration
	Timeout       time.Duration
}

func NewHealthTracker(clients []*grpcClient, cfg HealthTrackerConfig) *HealthTracker {
	if cfg.CheckInterval == 0 {
		cfg.CheckInterval = 5 * time.Second
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = 2 * time.Second
	}

	status := make(map[string]*WorkerHealth, len(clients))
	for _, c := range clients {
		status[c.address] = &WorkerHealth{
			Healthy:   true,
			LastCheck: time.Now(),
		}
	}

	return &HealthTracker{
		status:        status,
		clients:       clients,
		checkInterval: cfg.CheckInterval,
		timeout:       cfg.Timeout,
	}
}

func (h *HealthTracker) Start(ctx context.Context) {
	ticker := time.NewTicker(h.checkInterval)
	defer ticker.Stop()

	h.checkAllWorkers(ctx)

	for {
		select {
		case <-ticker.C:
			h.checkAllWorkers(ctx)
		case <-ctx.Done():
			slog.Info("health tracker stopped")
			return
		}
	}
}

func (h *HealthTracker) checkAllWorkers(ctx context.Context) {
	for _, client := range h.clients {
		go h.checkWorker(ctx, client)
	}
}

func (h *HealthTracker) checkWorker(ctx context.Context, client *grpcClient) {
	checkCtx, cancel := context.WithTimeout(ctx, h.timeout)
	defer cancel()

	start := time.Now()
	resp, err := client.rpcClient.Health(checkCtx, &pb.HealthRequest{})
	latency := time.Since(start).Seconds() * 1000

	h.mu.Lock()
	defer h.mu.Unlock()

	status, exists := h.status[client.address]
	if !exists {
		status = &WorkerHealth{}
		h.status[client.address] = status
	}

	status.LastCheck = time.Now()
	status.AvgLatencyMs = (status.AvgLatencyMs + latency) / 2

	if err != nil {
		status.ErrorCount++
		status.ConsecutiveErr++

		if status.ConsecutiveErr >= 3 {
			status.Healthy = false
			client.healthy.Store(false)
			slog.Warn("worker marked unhealthy",
				"address", client.address,
				"consecutive_errors", status.ConsecutiveErr,
			)
		}

		metrics.WorkerHealthGauge.WithLabelValues(client.address).Set(0)
		return
	}

	status.Healthy = resp.Healthy
	status.GPUUtilization = resp.GpuUtilization
	status.QueueDepth = int(resp.CurrentQueueSize)
	status.ConsecutiveErr = 0
	client.healthy.Store(resp.Healthy)

	// GPU memory stats (for memory-aware routing)
	status.GPUMemoryUsed = resp.GpuMemoryUsed
	status.GPUMemoryTotal = resp.GpuMemoryTotal

	// Build topology from health response
	if resp.GpuCount > 0 || resp.GpuType != "" {
		status.Topology = &WorkerTopology{
			GPUCount:     int(resp.GpuCount),
			GPUType:      resp.GpuType,
			Interconnect: InterconnectType(resp.InterconnectType),
			IBAvailable:  resp.IbAvailable,
			IBState:      IBState(resp.IbState),
			IBWidth:      int(resp.IbWidth),
			IBSpeed:      IBSpeed(resp.IbSpeed),
		}

		// Calculate total memory if per-GPU memory known
		if status.GPUMemoryTotal > 0 {
			status.Topology.TotalGPUMemory = status.GPUMemoryTotal
		} else if gpuMem := KnownGPUMemory(resp.GpuType); gpuMem > 0 {
			status.Topology.GPUMemory = gpuMem
			status.Topology.TotalGPUMemory = gpuMem * int64(resp.GpuCount)
		}
	}

	healthVal := 0.0
	if resp.Healthy {
		healthVal = 1.0
	}
	metrics.WorkerHealthGauge.WithLabelValues(client.address).Set(healthVal)
	metrics.WorkerGPUUtilization.WithLabelValues(client.address).Set(float64(resp.GpuUtilization))
	metrics.WorkerQueueDepth.WithLabelValues(client.address).Set(float64(resp.CurrentQueueSize))

	slog.Debug("health check completed",
		"address", client.address,
		"healthy", resp.Healthy,
		"gpu_util", resp.GpuUtilization,
		"queue", resp.CurrentQueueSize,
		"gpu_mem_used", resp.GpuMemoryUsed,
		"gpu_mem_total", resp.GpuMemoryTotal,
		"ib_state", resp.IbState,
		"latency_ms", latency,
	)
}

func (h *HealthTracker) Get(address string) *WorkerHealth {
	h.mu.RLock()
	defer h.mu.RUnlock()

	if status, ok := h.status[address]; ok {
		// Return a copy to avoid data race (caller reads while checkWorker writes)
		copy := *status
		if status.Topology != nil {
			topoCopy := *status.Topology
			copy.Topology = &topoCopy
		}
		return &copy
	}
	return &WorkerHealth{Healthy: false}
}

func (h *HealthTracker) IsHealthy(address string) bool {
	return h.Get(address).Healthy
}

func (h *HealthTracker) GetAll() map[string]*WorkerHealth {
	h.mu.RLock()
	defer h.mu.RUnlock()

	result := make(map[string]*WorkerHealth, len(h.status))
	for k, v := range h.status {
		copy := *v
		result[k] = &copy
	}
	return result
}
