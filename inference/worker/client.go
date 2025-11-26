package worker

import (
	"context"
	"io"
	"log"
	"time"

	pb "github.com/aluko123/go-network-proxy/inference/pb"
	"github.com/aluko123/go-network-proxy/inference/queue"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

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
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

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
		log.Printf("[%s] Error starting stream: %v", c.ID, err)
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
			log.Printf("[%s] Stream broken: %v", c.ID, err)
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
