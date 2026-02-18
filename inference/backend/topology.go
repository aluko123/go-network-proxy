package backend

// GPU and interconnect topology for intelligent routing.
// Understanding these concepts is critical for ML Platform Engineering:
//
// NCCL (NVIDIA Collective Communications Library):
//   - Handles multi-GPU communication: allreduce, broadcast, allgather
//   - Ring allreduce: GPUs form ring, O(n) steps, good for small clusters
//   - Tree allreduce: Hierarchical, better for large clusters
//   - Key env: NCCL_DEBUG=INFO, NCCL_IB_DISABLE, NCCL_SOCKET_IFNAME
//
// Interconnect hierarchy (fastest to slowest):
//   1. NVLink: 600-900 GB/s (H100), direct GPU-to-GPU
//   2. NVSwitch: Full NVLink mesh within a node (DGX)
//   3. InfiniBand: 200-400 Gbps (HDR/NDR), across nodes
//   4. PCIe: 64 GB/s (Gen5 x16), slowest for GPU-GPU
//   5. Ethernet: 100-400 Gbps, high latency, avoid for training
//
// Why this matters for routing:
//   - Distributed inference needs fast interconnect
//   - Tensor parallelism requires NVLink (intra-op communication)
//   - Data parallelism works over IB (gradient sync less frequent)
//   - Memory-constrained workers should reject large context requests

// InterconnectType represents the GPU interconnect available
type InterconnectType string

const (
	InterconnectNVLink   InterconnectType = "nvlink"   // Direct GPU-GPU, fastest
	InterconnectNVSwitch InterconnectType = "nvswitch" // Full mesh NVLink (DGX)
	InterconnectPCIe     InterconnectType = "pcie"     // Through CPU, slower
	InterconnectNone     InterconnectType = "none"     // Single GPU, no interconnect
)

// IBState represents InfiniBand port state
type IBState string

const (
	IBStateActive IBState = "Active" // Ready for traffic
	IBStateInit   IBState = "Init"   // Physical up, no SM connection
	IBStateDown   IBState = "Down"   // Link down
)

// IBSpeed represents InfiniBand speed generations
type IBSpeed string

const (
	IBSpeedEDR IBSpeed = "EDR" // 100 Gbps (25 GB/s per port)
	IBSpeedHDR IBSpeed = "HDR" // 200 Gbps (50 GB/s per port)
	IBSpeedNDR IBSpeed = "NDR" // 400 Gbps (100 GB/s per port)
)

// WorkerTopology describes a worker's hardware configuration.
// Used for intelligent routing decisions:
//   - Route large models to workers with more GPU memory
//   - Route distributed inference to IB-connected workers
//   - Prefer NVLink workers for tensor-parallel inference
type WorkerTopology struct {
	// GPU configuration
	GPUCount  int    // Number of GPUs on this worker
	GPUType   string // e.g., "A100-80GB", "H100-80GB"
	GPUMemory int64  // Per-GPU memory in bytes

	// Intra-node interconnect
	Interconnect InterconnectType // How GPUs communicate within node

	// Inter-node networking (InfiniBand)
	IBAvailable bool    // Is InfiniBand present?
	IBState     IBState // Port state
	IBWidth     int     // Link width (typically 4 for 4x)
	IBSpeed     IBSpeed // Generation (EDR/HDR/NDR)

	// Computed fields
	TotalGPUMemory int64 // GPUCount * GPUMemory
	IBBandwidthGBs int   // Effective bandwidth in GB/s
}

// CanHandleDistributed returns true if worker can participate in
// distributed inference/training (requires active InfiniBand)
func (t *WorkerTopology) CanHandleDistributed() bool {
	return t.IBAvailable && t.IBState == IBStateActive
}

// CanHandleTensorParallel returns true if worker has fast GPU interconnect
// needed for tensor parallelism (frequent intra-op communication)
func (t *WorkerTopology) CanHandleTensorParallel() bool {
	return t.Interconnect == InterconnectNVLink || t.Interconnect == InterconnectNVSwitch
}

// EstimatedIBBandwidth returns theoretical IB bandwidth in GB/s
// based on speed and width
func (t *WorkerTopology) EstimatedIBBandwidth() int {
	if !t.IBAvailable {
		return 0
	}

	// Base speed per lane
	var speedPerLane int
	switch t.IBSpeed {
	case IBSpeedEDR:
		speedPerLane = 25 // Gbps
	case IBSpeedHDR:
		speedPerLane = 50
	case IBSpeedNDR:
		speedPerLane = 100
	default:
		speedPerLane = 25
	}

	// Width multiplier (typically 4x)
	width := t.IBWidth
	if width == 0 {
		width = 4
	}

	// Total Gbps, convert to GB/s (divide by 8)
	return (speedPerLane * width) / 8
}

// MemoryScore returns a score based on available GPU memory.
// Higher is better. Used in routing decisions.
func (t *WorkerTopology) MemoryScore(memoryUsed int64) float64 {
	if t.TotalGPUMemory == 0 {
		return 0
	}
	available := t.TotalGPUMemory - memoryUsed
	return float64(available) / float64(t.TotalGPUMemory) * 100
}

// KnownGPUMemory returns the typical memory in bytes for known GPU types.
// Used when workers don't report memory directly.
func KnownGPUMemory(gpuType string) int64 {
	// Memory in bytes
	const GB = 1024 * 1024 * 1024

	known := map[string]int64{
		// A100 variants
		"A100-40GB":     40 * GB,
		"A100-80GB":     80 * GB,
		"A100-SXM4-40G": 40 * GB,
		"A100-SXM4-80G": 80 * GB,

		// H100 variants
		"H100-80GB":     80 * GB,
		"H100-SXM5-80G": 80 * GB,
		"H100-PCIe":     80 * GB,

		// Older/smaller
		"V100-16GB": 16 * GB,
		"V100-32GB": 32 * GB,
		"A10":       24 * GB,
		"L4":        24 * GB,
		"L40":       48 * GB,
		"L40S":      48 * GB,

		// Consumer (for dev/test)
		"RTX4090": 24 * GB,
		"RTX3090": 24 * GB,
	}

	if mem, ok := known[gpuType]; ok {
		return mem
	}
	return 0
}

// EstimateKVCacheMemory estimates memory needed for KV cache based on
// model and context length. This is approximate and model-specific.
//
// Formula (transformer KV cache):
//   memory = 2 * num_layers * hidden_dim * num_heads * seq_len * bytes_per_element
//
// Simplified: ~2MB per 1K tokens for 7B model, scales with model size
func EstimateKVCacheMemory(modelName string, contextLength int) int64 {
	// Bytes per 1K tokens (approximate)
	bytesPerKTokens := map[string]int64{
		// 7B class models
		"llama-7b":   2 * 1024 * 1024, // 2 MB per 1K tokens
		"mistral-7b": 2 * 1024 * 1024,

		// 13B class
		"llama-13b": 4 * 1024 * 1024,

		// 70B class
		"llama-70b":  20 * 1024 * 1024, // 20 MB per 1K tokens
		"llama-2-70b": 20 * 1024 * 1024,

		// Default for unknown models
		"default": 5 * 1024 * 1024,
	}

	bytesPerK, ok := bytesPerKTokens[modelName]
	if !ok {
		bytesPerK = bytesPerKTokens["default"]
	}

	// Context in K tokens
	kTokens := float64(contextLength) / 1000.0

	return int64(float64(bytesPerK) * kTokens)
}
