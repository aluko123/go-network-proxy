package backend

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	pb "github.com/aluko123/go-network-proxy/inference/pb"
	"github.com/aluko123/go-network-proxy/pkg/metrics"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
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
}

type grpcClient struct {
	address   string
	conn      *grpc.ClientConn
	rpcClient pb.ModelServiceClient
	healthy   atomic.Bool
}

type GRPCConfig struct {
	Name           string
	Addresses      []string
	Models         []string
	CacheHitWeight float64 // Bonus for prefix cache hits (default: 20)
	PrefixTTL      time.Duration
}

func NewGRPCBackend(cfg GRPCConfig) (*GRPCBackend, error) {
	cacheHitWeight := cfg.CacheHitWeight
	if cacheHitWeight == 0 {
		cacheHitWeight = 20 // Default: cache hit worth 20% GPU utilization difference
	}

	b := &GRPCBackend{
		name:           cfg.Name,
		addresses:      cfg.Addresses,
		models:         cfg.Models,
		clients:        make([]*grpcClient, 0, len(cfg.Addresses)),
		cacheHitWeight: cacheHitWeight,
		prefixIndex:    NewPrefixIndex(PrefixIndexConfig{TTL: cfg.PrefixTTL}),
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
			address:   addr,
			conn:      conn,
			rpcClient: pb.NewModelServiceClient(conn),
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

	var best *grpcClient
	var bestScore float64 = -1

	for _, client := range g.clients {
		health := g.tracker.Get(client.address)
		if !health.Healthy {
			continue
		}

		// Base score: lower GPU util + lower queue = better
		// GPU util is 0-100, queue depth weighted by 10
		score := 100 - float64(health.GPUUtilization) - float64(health.QueueDepth*10)

		// Prefix affinity bonus: if this worker likely has the prefix cached
		if prefixHash != "" && g.prefixIndex.HasWorker(prefixHash, client.address) {
			score += g.cacheHitWeight
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
		return best
	}

	// Fallback: round-robin among any available
	for i := 0; i < len(g.clients); i++ {
		idx := g.nextIdx.Add(1) % uint64(len(g.clients))
		client := g.clients[idx]
		if client.healthy.Load() {
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
			client.healthy.Store(false)
			tokenCh <- Token{RequestID: req.ID, Error: err, Finished: true}
			return
		}

		client.healthy.Store(true)

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
				client.healthy.Store(false)
				tokenCh <- Token{RequestID: req.ID, Error: err, Finished: true}
				return
			}

			if resp.Error != "" {
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
				return
			}
		}
	}()

	return tokenCh, nil
}
