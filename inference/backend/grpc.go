package backend

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"

	pb "github.com/aluko123/go-network-proxy/inference/pb"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type GRPCBackend struct {
	name      string
	addresses []string
	models    []string
	clients   []*grpcClient
	healthy   atomic.Bool
	nextIdx   atomic.Uint64
	mu        sync.RWMutex
}

type grpcClient struct {
	address   string
	conn      *grpc.ClientConn
	rpcClient pb.ModelServiceClient
	healthy   atomic.Bool
}

type GRPCConfig struct {
	Name      string
	Addresses []string
	Models    []string
}

func NewGRPCBackend(cfg GRPCConfig) (*GRPCBackend, error) {
	b := &GRPCBackend{
		name:      cfg.Name,
		addresses: cfg.Addresses,
		models:    cfg.Models,
		clients:   make([]*grpcClient, 0, len(cfg.Addresses)),
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

	return b, nil
}

func (g *GRPCBackend) Name() string     { return g.name }
func (g *GRPCBackend) Type() string     { return "grpc" }
func (g *GRPCBackend) Models() []string { return g.models }
func (g *GRPCBackend) Healthy() bool    { return g.healthy.Load() }

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

func (g *GRPCBackend) getClient() *grpcClient {
	g.mu.RLock()
	defer g.mu.RUnlock()

	if len(g.clients) == 0 {
		return nil
	}

	// Round-robin with health check
	for i := 0; i < len(g.clients); i++ {
		idx := g.nextIdx.Add(1) % uint64(len(g.clients))
		client := g.clients[idx]
		if client.healthy.Load() {
			return client
		}
	}

	// Fall back to any client
	return g.clients[0]
}

func (g *GRPCBackend) Generate(ctx context.Context, req *Request) (<-chan Token, error) {
	tokenCh := make(chan Token, 100)

	client := g.getClient()
	if client == nil {
		close(tokenCh)
		return nil, io.EOF
	}

	go func() {
		defer close(tokenCh)

		rpcReq := &pb.GenerateRequest{
			RequestId:   req.ID,
			Model:       req.Model,
			Prompt:      req.Prompt,
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
