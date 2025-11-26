package worker

import (
	"context"
	"io"
	"log/slog"
	"time"

	pb "github.com/aluko123/go-network-proxy/inference/pb"
	"github.com/aluko123/go-network-proxy/inference/queue"
	"github.com/aluko123/go-network-proxy/pkg/metrics"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// Config holds worker client configuration
type Config struct {
	InferenceTimeout time.Duration
}

// DefaultConfig returns the default worker configuration
func DefaultConfig() Config {
	return Config{
		InferenceTimeout: 5 * time.Minute,
	}
}

var config = DefaultConfig()

// SetConfig updates the worker configuration
func SetConfig(c Config) {
	config = c
}

// Client manages a connection to a single Python worker
type Client struct {
	ID        string
	conn      *grpc.ClientConn
	rpcClient pb.ModelServiceClient
	Address   string
	Healthy   bool
}

// NewClient creates a new worker client
func NewClient(id, address string) (*Client, error) {
	// Connect to the Python worker
	// Modern gRPC uses NewClient and defaults to non-blocking (lazy) connection
	conn, err := grpc.NewClient(address,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return nil, err
	}

	return &Client{
		ID:        id,
		conn:      conn,
		rpcClient: pb.NewModelServiceClient(conn),
		Address:   address,
		Healthy:   true,
	}, nil
}

// ProcessRequest takes a request from the queue and streams it to the worker
func (c *Client) ProcessRequest(req *queue.Request) {
	ctx, cancel := context.WithTimeout(context.Background(), config.InferenceTimeout)
	defer cancel()

	// Mark processing start time and record queue wait
	req.StartTime = time.Now()
	priorityLabel := metrics.PriorityLabel(req.Priority)
	metrics.InferenceQueueWaitDuration.WithLabelValues(req.Model, priorityLabel).Observe(req.StartTime.Sub(req.SubmitTime).Seconds())

	status := "success"

	defer func() {
		// Record processing duration
		metrics.InferenceProcessingDuration.WithLabelValues(req.Model, c.ID).Observe(time.Since(req.StartTime).Seconds())
		// Record worker request count
		metrics.InferenceWorkerRequestsTotal.WithLabelValues(c.ID, status).Inc()
	}()

	// Create gRPC request
	rpcReq := &pb.GenerateRequest{
		RequestId:   req.ID,
		Model:       req.Model,
		Prompt:      req.Prompt,
		MaxTokens:   int32(req.MaxTokens),
		Temperature: req.Temperature,
		Priority:    int32(req.Priority),
	}

	// Start streaming
	stream, err := c.rpcClient.Generate(ctx, rpcReq)
	if err != nil {
		status = "error"
		slog.Error("stream error", "worker_id", c.ID, "error", err)
		req.ErrorCh <- err
		return
	}

	// Read stream
	for {
		resp, err := stream.Recv()
		if err == io.EOF {
			close(req.ResponseCh)
			return
		}
		if err != nil {
			status = "error"
			slog.Error("stream broken", "worker_id", c.ID, "error", err)
			req.ErrorCh <- err
			return
		}

		// Forward token
		req.ResponseCh <- resp
	}
}

// Close terminates the connection
func (c *Client) Close() error {
	return c.conn.Close()
}
