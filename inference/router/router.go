package router

import (
	"fmt"
	"log"

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
		log.Printf("✓ Connected to worker %s (%s)", id, addr)
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
	log.Printf("[%s] Starting processing loop...", w.ID)
	for {
		// 1. Block until a request is available
		req := r.queue.Pop()

		// 2. Process it
		// log.Printf("[%s] Processing Request %s", w.ID, req.ID)
		w.ProcessRequest(req)
	}
}

// Close shuts down all workers
func (r *Router) Close() {
	for _, w := range r.workers {
		w.Close()
	}
}
