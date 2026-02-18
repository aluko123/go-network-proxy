package backend

import "testing"

func TestWorkerTopology_CanHandleDistributed(t *testing.T) {
	tests := []struct {
		name     string
		topology WorkerTopology
		want     bool
	}{
		{
			name: "IB active",
			topology: WorkerTopology{
				IBAvailable: true,
				IBState:     IBStateActive,
			},
			want: true,
		},
		{
			name: "IB init (no SM)",
			topology: WorkerTopology{
				IBAvailable: true,
				IBState:     IBStateInit,
			},
			want: false,
		},
		{
			name: "IB down",
			topology: WorkerTopology{
				IBAvailable: true,
				IBState:     IBStateDown,
			},
			want: false,
		},
		{
			name: "no IB",
			topology: WorkerTopology{
				IBAvailable: false,
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.topology.CanHandleDistributed(); got != tt.want {
				t.Errorf("CanHandleDistributed() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestWorkerTopology_CanHandleTensorParallel(t *testing.T) {
	tests := []struct {
		name     string
		topology WorkerTopology
		want     bool
	}{
		{
			name:     "NVLink",
			topology: WorkerTopology{Interconnect: InterconnectNVLink},
			want:     true,
		},
		{
			name:     "NVSwitch",
			topology: WorkerTopology{Interconnect: InterconnectNVSwitch},
			want:     true,
		},
		{
			name:     "PCIe only",
			topology: WorkerTopology{Interconnect: InterconnectPCIe},
			want:     false,
		},
		{
			name:     "single GPU",
			topology: WorkerTopology{Interconnect: InterconnectNone},
			want:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.topology.CanHandleTensorParallel(); got != tt.want {
				t.Errorf("CanHandleTensorParallel() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestWorkerTopology_EstimatedIBBandwidth(t *testing.T) {
	tests := []struct {
		name     string
		topology WorkerTopology
		wantGBs  int
	}{
		{
			name: "HDR 4x",
			topology: WorkerTopology{
				IBAvailable: true,
				IBSpeed:     IBSpeedHDR,
				IBWidth:     4,
			},
			wantGBs: 25, // 50 Gbps * 4 lanes / 8 = 25 GB/s
		},
		{
			name: "NDR 4x",
			topology: WorkerTopology{
				IBAvailable: true,
				IBSpeed:     IBSpeedNDR,
				IBWidth:     4,
			},
			wantGBs: 50, // 100 Gbps * 4 lanes / 8 = 50 GB/s
		},
		{
			name: "EDR 4x",
			topology: WorkerTopology{
				IBAvailable: true,
				IBSpeed:     IBSpeedEDR,
				IBWidth:     4,
			},
			wantGBs: 12, // 25 Gbps * 4 lanes / 8 = 12.5 GB/s (truncated)
		},
		{
			name: "no IB",
			topology: WorkerTopology{
				IBAvailable: false,
			},
			wantGBs: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.topology.EstimatedIBBandwidth(); got != tt.wantGBs {
				t.Errorf("EstimatedIBBandwidth() = %v GB/s, want %v GB/s", got, tt.wantGBs)
			}
		})
	}
}

func TestKnownGPUMemory(t *testing.T) {
	const GB = 1024 * 1024 * 1024

	tests := []struct {
		gpuType string
		wantGB  int64
	}{
		{"A100-80GB", 80},
		{"A100-40GB", 40},
		{"H100-80GB", 80},
		{"V100-32GB", 32},
		{"RTX4090", 24},
		{"unknown-gpu", 0},
	}

	for _, tt := range tests {
		t.Run(tt.gpuType, func(t *testing.T) {
			got := KnownGPUMemory(tt.gpuType)
			want := tt.wantGB * GB
			if got != want {
				t.Errorf("KnownGPUMemory(%s) = %d, want %d", tt.gpuType, got, want)
			}
		})
	}
}

func TestEstimateKVCacheMemory(t *testing.T) {
	tests := []struct {
		model         string
		contextLength int
		wantMBApprox  int64 // Approximate, check within range
	}{
		{"llama-7b", 1000, 2},    // ~2MB per 1K tokens
		{"llama-7b", 4000, 8},    // ~8MB for 4K
		{"llama-70b", 1000, 20},  // ~20MB per 1K tokens
		{"llama-70b", 32000, 640}, // ~640MB for 32K context
		{"unknown-model", 1000, 5}, // default ~5MB per 1K
	}

	for _, tt := range tests {
		t.Run(tt.model, func(t *testing.T) {
			got := EstimateKVCacheMemory(tt.model, tt.contextLength)
			wantBytes := tt.wantMBApprox * 1024 * 1024

			// Allow 10% variance
			low := wantBytes * 90 / 100
			high := wantBytes * 110 / 100

			if got < low || got > high {
				t.Errorf("EstimateKVCacheMemory(%s, %d) = %d bytes, want ~%d bytes",
					tt.model, tt.contextLength, got, wantBytes)
			}
		})
	}
}

func TestWorkerTopology_MemoryScore(t *testing.T) {
	const GB = int64(1024 * 1024 * 1024)

	topology := WorkerTopology{
		TotalGPUMemory: 80 * GB,
	}

	tests := []struct {
		name       string
		memoryUsed int64
		wantScore  float64
	}{
		{"empty", 0, 100.0},
		{"half full", 40 * GB, 50.0},
		{"75% full", 60 * GB, 25.0},
		{"full", 80 * GB, 0.0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := topology.MemoryScore(tt.memoryUsed)
			if got != tt.wantScore {
				t.Errorf("MemoryScore(%d) = %v, want %v", tt.memoryUsed, got, tt.wantScore)
			}
		})
	}
}
