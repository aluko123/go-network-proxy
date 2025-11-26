package queue

import (
	"container/heap"
	"sync"
	"time"

	pb "github.com/aluko123/go-network-proxy/inference/pb"
	"github.com/aluko123/go-network-proxy/pkg/metrics"
)

// Request represents an inference request in the queue
type Request struct {
	ID          string
	Model       string
	Prompt      string
	MaxTokens   int
	Temperature float32
	Priority    int // Higher number = Higher priority
	SubmitTime  time.Time
	StartTime   time.Time // When worker began processing

	// Channels for response handling
	ResponseCh chan *pb.TokenResponse
	ErrorCh    chan error

	// Internal heap index
	index int
}

// RequestHeap implements heap.Interface
type RequestHeap []*Request

func (h RequestHeap) Len() int { return len(h) }

func (h RequestHeap) Less(i, j int) bool {
	// 1. Priority Check (Higher is better)
	if h[i].Priority != h[j].Priority {
		return h[i].Priority > h[j].Priority
	}
	// 2. FIFO Fallback (Older is better)
	return h[i].SubmitTime.Before(h[j].SubmitTime)
}

func (h RequestHeap) Swap(i, j int) {
	h[i], h[j] = h[j], h[i]
	h[i].index = i
	h[j].index = j
}

func (h *RequestHeap) Push(x interface{}) {
	n := len(*h)
	item := x.(*Request)
	item.index = n
	*h = append(*h, item)
}

func (h *RequestHeap) Pop() interface{} {
	old := *h
	n := len(old)
	item := old[n-1]
	old[n-1] = nil // avoid memory leak
	item.index = -1
	*h = old[0 : n-1]
	return item
}

// PriorityQueue manages the request heap in a thread-safe way
type PriorityQueue struct {
	items    RequestHeap
	mu       sync.Mutex
	cond     *sync.Cond
	closed   bool
	inflight sync.WaitGroup
}

func NewPriorityQueue() *PriorityQueue {
	pq := &PriorityQueue{
		items: make(RequestHeap, 0),
	}
	pq.cond = sync.NewCond(&pq.mu)
	heap.Init(&pq.items)
	return pq
}

// Push adds a request to the queue
func (pq *PriorityQueue) Push(req *Request) bool {
	pq.mu.Lock()
	defer pq.mu.Unlock()

	if pq.closed {
		return false
	}

	pq.inflight.Add(1)
	heap.Push(&pq.items, req)
	metrics.InferenceQueueDepth.Set(float64(len(pq.items)))
	pq.cond.Signal() // Wake up a worker
	return true
}

// Pop blocks until a request is available, then returns the highest priority one
// Returns nil if the queue is closed and empty
func (pq *PriorityQueue) Pop() *Request {
	pq.mu.Lock()
	defer pq.mu.Unlock()

	for len(pq.items) == 0 && !pq.closed {
		pq.cond.Wait()
	}

	if len(pq.items) == 0 {
		return nil
	}

	item := heap.Pop(&pq.items).(*Request)
	metrics.InferenceQueueDepth.Set(float64(len(pq.items)))
	metrics.InferenceInFlight.Inc()
	return item
}

// Done marks a request as completed (call after processing)
func (pq *PriorityQueue) Done() {
	metrics.InferenceInFlight.Dec()
	pq.inflight.Done()
}

// Len returns current queue depth
func (pq *PriorityQueue) Len() int {
	pq.mu.Lock()
	defer pq.mu.Unlock()
	return len(pq.items)
}

// Close stops accepting new requests and signals workers to drain
func (pq *PriorityQueue) Close() {
	pq.mu.Lock()
	pq.closed = true
	pq.cond.Broadcast() // Wake up all waiting workers
	pq.mu.Unlock()
}

// Wait blocks until all in-flight requests are processed
func (pq *PriorityQueue) Wait() {
	pq.inflight.Wait()
}
