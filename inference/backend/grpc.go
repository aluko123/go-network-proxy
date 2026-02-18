package backend

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	pb "github.com/aluko123/go-network-proxy/inference/pb"
	"github.com/aluko123/go-network-proxy/pkg/metrics"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
)

type GRPCBackend struct {
	name           string
	addresses      []string
	models         []string
	clients        []*grpcClient
	healthy        atomic.Bool
	nextIdx        atomic.Uint64
	mu             sync.RWMutex
	tracker        *HealthTracker
	prefixIndex    *PrefixIndex
	cacheHitWeight float64 // Bonus score for workers with cached prefix (default: 20)
	memoryWeight   float64 // Weight for memory headroom in scoring (default: 10)
	memoryMargin   int64   // Safety margin in bytes to keep free (default: 2GB)
	breakerConfig  breakerConfig
}

type grpcClient struct {
	address            string
	conn               *grpc.ClientConn
	rpcClient          pb.ModelServiceClient
	healthy            atomic.Bool
	breakerMu          sync.Mutex
	breakerState       breakerState
	consecutiveFailure int
	openUntil          time.Time
}

type GRPCConfig struct {
	Name                    string
	Addresses               []string
	Models                  []string
	CacheHitWeight          float64       // Bonus for prefix cache hits (default: 20)
	MemoryWeight            float64       // Weight for memory headroom scoring (default: 10)
	MemoryMargin            int64         // Safety margin bytes to keep free (default: 2GB)
	PrefixTTL               time.Duration
	CircuitFailureThreshold int           // Consecutive failures before opening breaker
	CircuitOpenTimeout      time.Duration // How long to keep breaker open
}

type breakerState int

const (
	breakerClosed breakerState = iota
	breakerOpen
)

type breakerConfig struct {
	failureThreshold int
	openTimeout      time.Duration
}

func NewGRPCBackend(cfg GRPCConfig) (*GRPCBackend, error) {
	cacheHitWeight := cfg.CacheHitWeight
	if cacheHitWeight == 0 {
		cacheHitWeight = 20 // Default: cache hit worth 20% GPU utilization difference
	}

	memoryWeight := cfg.MemoryWeight
	if memoryWeight == 0 {
		memoryWeight = 10 // Default: memory headroom bonus weight
	}

	memoryMargin := cfg.MemoryMargin
	if memoryMargin == 0 {
		memoryMargin = 2 * 1024 * 1024 * 1024 // Default: 2GB safety margin
	}

	failureThreshold := cfg.CircuitFailureThreshold
	if failureThreshold == 0 {
		failureThreshold = 3
	}

	openTimeout := cfg.CircuitOpenTimeout
	if openTimeout == 0 {
		openTimeout = 30 * time.Second
	}

	b := &GRPCBackend{
		name:           cfg.Name,
		addresses:      cfg.Addresses,
		models:         cfg.Models,
		clients:        make([]*grpcClient, 0, len(cfg.Addresses)),
		cacheHitWeight: cacheHitWeight,
		memoryWeight:   memoryWeight,
		memoryMargin:   memoryMargin,
		prefixIndex:    NewPrefixIndex(PrefixIndexConfig{TTL: cfg.PrefixTTL}),
		breakerConfig: breakerConfig{
			failureThreshold: failureThreshold,
			openTimeout:      openTimeout,
		},
	}
	b.healthy.Store(true)

	for _, addr := range cfg.Addresses {
		conn, err := grpc.NewClient(addr,
			grpc.WithTransportCredentials(insecure.NewCredentials()),
		)
		if err != nil {
			slog.Warn("failed to connect to grpc worker", "address", addr, "error", err)
			continue
		}

		client := &grpcClient{
			address:      addr,
			conn:         conn,
			rpcClient:    pb.NewModelServiceClient(conn),
			breakerState: breakerClosed,
		}
		client.healthy.Store(true)
		b.clients = append(b.clients, client)
		slog.Info("connected to grpc worker", "backend", cfg.Name, "address", addr)
	}

	if len(b.clients) == 0 {
		b.healthy.Store(false)
	}

	b.tracker = NewHealthTracker(b.clients, HealthTrackerConfig{})

	return b, nil
}

func (g *GRPCBackend) StartHealthChecks(ctx context.Context) {
	go g.tracker.Start(ctx)
}

func (g *GRPCBackend) Name() string     { return g.name }
func (g *GRPCBackend) Type() string     { return "grpc" }
func (g *GRPCBackend) Models() []string { return g.models }
func (g *GRPCBackend) Healthy() bool    { return g.healthy.Load() }

func (g *GRPCBackend) PrefixStats() (prefixCount, workerMappings int) {
	return g.prefixIndex.Stats()
}

func (g *GRPCBackend) Close() error {
	g.mu.Lock()
	defer g.mu.Unlock()

	for _, c := range g.clients {
		if err := c.conn.Close(); err != nil {
			slog.Error("error closing grpc connection", "address", c.address, "error", err)
		}
	}
	return nil
}

func (g *GRPCBackend) getClientForRequest(req *Request) *grpcClient {
	g.mu.RLock()
	defer g.mu.RUnlock()

	if len(g.clients) == 0 {
		return nil
	}

	var prefixHash string
	var hasCacheHit bool
	if req.Prefix != "" {
		prefixHash = HashPrefix(req.Model, req.Prefix)
	}

	// Estimate memory this request will need for KV cache
	// Context length approximated from prompt + prefix length (chars / 4 ≈ tokens)
	contextLength := (len(req.Prompt) + len(req.Prefix)) / 4
	contextLength = max(contextLength, 100)
	estimatedMemory := EstimateKVCacheMemory(req.Model, contextLength)

	var best *grpcClient
	var bestScore float64 = -1
	var bestUtil float64
	var bestQueueDepth float64
	var bestQueuePenalty float64
	var bestUtilPenalty float64
	var bestMemoryFreeRatio float64
	var bestMemoryBonus float64
	var bestPrefixBonus float64
	var skippedForMemory int
	var skippedForIB int

	for _, client := range g.clients {
		if g.breakerOpen(client) {
			slog.Warn("worker skipped: circuit breaker open",
				"worker", client.address,
				"request_id", req.ID,
			)
			continue
		}

		health := g.tracker.Get(client.address)
		if !health.Healthy {
			continue
		}

		// InfiniBand check: distributed requests require active IB
		// This is critical for multi-node training/inference with NCCL
		if req.RequiresDistributed && health.Topology != nil {
			if !health.Topology.CanHandleDistributed() {
				skippedForIB++
				slog.Debug("worker skipped: IB not active for distributed request",
					"worker", client.address,
					"ib_available", health.Topology.IBAvailable,
					"ib_state", health.Topology.IBState,
					"request_id", req.ID,
				)
				continue
			}
		}

		// Tensor parallelism check: requires fast GPU interconnect (NVLink/NVSwitch)
		if req.TensorParallel > 1 && health.Topology != nil {
			if !health.Topology.CanHandleTensorParallel() {
				slog.Debug("worker skipped: no NVLink for tensor parallel request",
					"worker", client.address,
					"interconnect", health.Topology.Interconnect,
					"tensor_parallel", req.TensorParallel,
					"request_id", req.ID,
				)
				continue
			}
		}

		// Memory-aware routing: skip workers that can't handle this request
		// Only apply if worker reports memory stats (GPUMemoryTotal > 0)
		if health.GPUMemoryTotal > 0 {
			memoryAvailable := health.GPUMemoryTotal - health.GPUMemoryUsed
			memoryNeeded := estimatedMemory + g.memoryMargin

			if memoryAvailable < memoryNeeded {
				skippedForMemory++
				slog.Debug("worker skipped: insufficient GPU memory",
					"worker", client.address,
					"available_gb", float64(memoryAvailable)/(1024*1024*1024),
					"needed_gb", float64(memoryNeeded)/(1024*1024*1024),
					"request_id", req.ID,
				)
				continue
			}
		}

		// Base score: lower GPU util + lower queue = better
		// GPU util is 0-100, queue depth weighted by 10
		utilPenalty := float64(health.GPUUtilization)
		queuePenalty := float64(health.QueueDepth * 10)
		score := 100 - utilPenalty - queuePenalty

		// Memory headroom bonus: prefer workers with more free memory
		memoryBonus := 0.0
		memoryFreeRatio := 0.0
		if health.GPUMemoryTotal > 0 {
			memoryFreeRatio = float64(health.GPUMemoryTotal-health.GPUMemoryUsed) / float64(health.GPUMemoryTotal)
			memoryBonus = memoryFreeRatio * g.memoryWeight
			score += memoryBonus
		}

		// Prefix affinity bonus: if this worker likely has the prefix cached
		prefixBonus := 0.0
		if prefixHash != "" && g.prefixIndex.HasWorker(prefixHash, client.address) {
			prefixBonus = g.cacheHitWeight
			score += prefixBonus
			hasCacheHit = true
			slog.Debug("prefix cache hit bonus",
				"worker", client.address,
				"prefix_hash", prefixHash[:8],
				"bonus", g.cacheHitWeight,
			)
		}

		if score > bestScore {
			bestScore = score
			best = client
			bestUtil = float64(health.GPUUtilization)
			bestQueueDepth = float64(health.QueueDepth)
			bestQueuePenalty = queuePenalty
			bestUtilPenalty = utilPenalty
			bestMemoryFreeRatio = memoryFreeRatio
			bestMemoryBonus = memoryBonus
			bestPrefixBonus = prefixBonus
		}
	}

	// Log if all workers were skipped due to constraints
	if best == nil {
		if skippedForIB > 0 {
			slog.Warn("all workers skipped: no active InfiniBand for distributed request",
				"request_id", req.ID,
				"workers_skipped_ib", skippedForIB,
				"workers_checked", len(g.clients),
			)
		}
		if skippedForMemory > 0 {
			slog.Warn("all workers skipped due to insufficient GPU memory",
				"request_id", req.ID,
				"estimated_memory_mb", estimatedMemory/(1024*1024),
				"workers_checked", len(g.clients),
			)
		}
	}

	// Record prefix cache metrics
	if prefixHash != "" {
		if hasCacheHit {
			metrics.PrefixCacheHits.WithLabelValues(req.Model).Inc()
		} else {
			metrics.PrefixCacheMisses.WithLabelValues(req.Model).Inc()
		}
	}

	if best != nil {
		slog.Info("routing decision",
			"request_id", req.ID,
			"worker", best.address,
			"prefix_hash", maskedPrefixHash(prefixHash),
			"score", bestScore,
			"gpu_util", bestUtil,
			"queue_depth", bestQueueDepth,
			"queue_penalty", bestQueuePenalty,
			"gpu_util_penalty", bestUtilPenalty,
			"memory_free_ratio", bestMemoryFreeRatio,
			"memory_bonus", bestMemoryBonus,
			"prefix_bonus", bestPrefixBonus,
		)
		return best
	}

	// Fallback: round-robin among any available (ignoring memory constraints)
	// This is last-resort: better to try than to reject completely
	for i := 0; i < len(g.clients); i++ {
		idx := g.nextIdx.Add(1) % uint64(len(g.clients))
		client := g.clients[idx]
		if client.healthy.Load() {
			slog.Warn("using fallback routing (memory constraints ignored)",
				"worker", client.address,
				"request_id", req.ID,
			)
			return client
		}
	}

	// Last resort: return nil (no healthy workers available)
	return nil
}

func (g *GRPCBackend) Generate(ctx context.Context, req *Request) (<-chan Token, error) {
	tokenCh := make(chan Token, 100)

	client := g.getClientForRequest(req)
	if client == nil {
		close(tokenCh)
		return nil, io.EOF
	}

	// Record prefix→worker mapping for future affinity routing
	if req.Prefix != "" {
		prefixHash := HashPrefix(req.Model, req.Prefix)
		g.prefixIndex.Record(prefixHash, client.address)

		// Update prefix cache gauges
		prefixCount, mappings := g.prefixIndex.Stats()
		metrics.PrefixCacheSize.Set(float64(prefixCount))
		metrics.PrefixCacheMappings.Set(float64(mappings))

		slog.Debug("recorded prefix routing",
			"request_id", req.ID,
			"prefix_hash", prefixHash[:8],
			"worker", client.address,
		)
	}

	go func() {
		defer close(tokenCh)

		rpcReq := &pb.GenerateRequest{
			RequestId:   req.ID,
			Model:       req.Model,
			Prompt:      req.Prompt,
			Prefix:      req.Prefix,
			MaxTokens:   int32(req.MaxTokens),
			Temperature: req.Temperature,
			Priority:    int32(req.Priority),
		}

		stream, err := client.rpcClient.Generate(ctx, rpcReq)
		if err != nil {
			g.recordFailure(client, err)
			client.healthy.Store(false)
			tokenCh <- Token{RequestID: req.ID, Error: err, Finished: true}
			return
		}

		client.healthy.Store(true)
		g.recordSuccess(client)

		for {
			resp, err := stream.Recv()
			if err == io.EOF {
				tokenCh <- Token{
					RequestID: req.ID,
					Finished:  true,
				}
				return
			}
			if err != nil {
				g.recordFailure(client, err)
				client.healthy.Store(false)
				tokenCh <- Token{RequestID: req.ID, Error: err, Finished: true}
				return
			}

			if resp.Error != "" {
				g.recordFailure(client, errors.New(resp.Error))
				tokenCh <- Token{
					RequestID: req.ID,
					Error:     io.ErrUnexpectedEOF,
					Finished:  true,
				}
				return
			}

			tokenCh <- Token{
				RequestID:  req.ID,
				Text:       resp.Token,
				TokenCount: int(resp.TokenCount),
				Finished:   resp.Finished,
			}

			if resp.Finished {
				g.recordSuccess(client)
				return
			}
		}
	}()

	return tokenCh, nil
}

func maskedPrefixHash(prefixHash string) string {
	if prefixHash == "" {
		return ""
	}
	if len(prefixHash) <= 8 {
		return prefixHash
	}
	return prefixHash[:8]
}

func (g *GRPCBackend) breakerOpen(client *grpcClient) bool {
	client.breakerMu.Lock()
	defer client.breakerMu.Unlock()

	if client.breakerState != breakerOpen {
		return false
	}
	if time.Now().After(client.openUntil) {
		client.breakerState = breakerClosed
		client.consecutiveFailure = 0
		return false
	}
	return true
}

func (g *GRPCBackend) recordFailure(client *grpcClient, err error) {
	if err == nil {
		return
	}
	if !isBreakerFailure(err) {
		return
	}

	client.breakerMu.Lock()
	defer client.breakerMu.Unlock()

	client.consecutiveFailure++
	if client.consecutiveFailure >= g.breakerConfig.failureThreshold {
		client.breakerState = breakerOpen
		client.openUntil = time.Now().Add(g.breakerConfig.openTimeout)
		slog.Warn("circuit breaker opened",
			"worker", client.address,
			"failures", client.consecutiveFailure,
			"open_until", client.openUntil.Format(time.RFC3339),
		)
	}
}

func (g *GRPCBackend) recordSuccess(client *grpcClient) {
	client.breakerMu.Lock()
	defer client.breakerMu.Unlock()

	if client.consecutiveFailure > 0 || client.breakerState != breakerClosed {
		client.consecutiveFailure = 0
		client.breakerState = breakerClosed
	}
}

func isBreakerFailure(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	st, ok := status.FromError(err)
	if !ok {
		return true
	}
	switch st.Code() {
	case codes.DeadlineExceeded, codes.ResourceExhausted, codes.Unavailable, codes.Internal, codes.Aborted:
		return true
	default:
		return false
	}
}
