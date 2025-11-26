package router

import (
	"fmt"
	"log/slog"

	"github.com/aluko123/go-network-proxy/inference/queue"
	"github.com/aluko123/go-network-proxy/inference/worker"
)

// Router manages the worker pool and request distribution
type Router struct {
	workers []*worker.Client
	queue   *queue.PriorityQueue
}

// NewRouter creates a router with the given worker addresses
func NewRouter(addresses []string, pq *queue.PriorityQueue) (*Router, error) {
	r := &Router{
		workers: make([]*worker.Client, 0, len(addresses)),
		queue:   pq,
	}

	for i, addr := range addresses {
		id := fmt.Sprintf("worker-%d", i)
		w, err := worker.NewClient(id, addr)
		if err != nil {
			return nil, fmt.Errorf("failed to connect to worker %s: %v", addr, err)
		}
		r.workers = append(r.workers, w)
		slog.Info("connected to worker", "worker_id", id, "addr", addr)
	}

	return r, nil
}

// Start begins the worker loops
func (r *Router) Start() {
	for _, w := range r.workers {
		go r.workerLoop(w)
	}
}

// workerLoop constantly pulls from the queue and processes requests
func (r *Router) workerLoop(w *worker.Client) {
	slog.Info("starting processing loop", "worker_id", w.ID)
	for {
		// 1. Block until a request is available (nil if queue closed)
		req := r.queue.Pop()
		if req == nil {
			slog.Info("worker stopping", "worker_id", w.ID)
			return
		}

		// 2. Process it
		w.ProcessRequest(req)
		r.queue.Done()
	}
}

// Close shuts down all workers
func (r *Router) Close() {
	// Close the queue first (stops accepting, signals workers)
	r.queue.Close()

	// Wait for in-flight requests to complete
	r.queue.Wait()

	// Close worker connections
	for _, w := range r.workers {
		w.Close()
	}
	slog.Info("all workers stopped")
}
