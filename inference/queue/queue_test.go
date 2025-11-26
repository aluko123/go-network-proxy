package queue

import (
	"sync"
	"testing"
	"time"
)

func TestPriorityQueue_BasicOrdering(t *testing.T) {
	pq := NewPriorityQueue()

	// Push requests with different priorities
	pq.Push(&Request{ID: "low", Priority: 1, SubmitTime: time.Now()})
	pq.Push(&Request{ID: "high", Priority: 10, SubmitTime: time.Now()})
	pq.Push(&Request{ID: "medium", Priority: 5, SubmitTime: time.Now()})

	// Should pop in priority order: high, medium, low
	req := pq.Pop()
	if req.ID != "high" {
		t.Errorf("expected 'high', got '%s'", req.ID)
	}

	req = pq.Pop()
	if req.ID != "medium" {
		t.Errorf("expected 'medium', got '%s'", req.ID)
	}

	req = pq.Pop()
	if req.ID != "low" {
		t.Errorf("expected 'low', got '%s'", req.ID)
	}
}

func TestPriorityQueue_FIFOForEqualPriority(t *testing.T) {
	pq := NewPriorityQueue()

	// Push requests with same priority but different times
	t1 := time.Now()
	t2 := t1.Add(time.Millisecond)
	t3 := t2.Add(time.Millisecond)

	pq.Push(&Request{ID: "first", Priority: 5, SubmitTime: t1})
	pq.Push(&Request{ID: "second", Priority: 5, SubmitTime: t2})
	pq.Push(&Request{ID: "third", Priority: 5, SubmitTime: t3})

	// Should pop in FIFO order: first, second, third
	req := pq.Pop()
	if req.ID != "first" {
		t.Errorf("expected 'first', got '%s'", req.ID)
	}

	req = pq.Pop()
	if req.ID != "second" {
		t.Errorf("expected 'second', got '%s'", req.ID)
	}

	req = pq.Pop()
	if req.ID != "third" {
		t.Errorf("expected 'third', got '%s'", req.ID)
	}
}

func TestPriorityQueue_MixedPriorityAndTime(t *testing.T) {
	pq := NewPriorityQueue()

	now := time.Now()

	// Mix of priorities and times
	pq.Push(&Request{ID: "old-low", Priority: 1, SubmitTime: now})
	pq.Push(&Request{ID: "new-high", Priority: 10, SubmitTime: now.Add(time.Second)})
	pq.Push(&Request{ID: "old-high", Priority: 10, SubmitTime: now})

	// High priority first, then by time within same priority
	req := pq.Pop()
	if req.ID != "old-high" {
		t.Errorf("expected 'old-high', got '%s'", req.ID)
	}

	req = pq.Pop()
	if req.ID != "new-high" {
		t.Errorf("expected 'new-high', got '%s'", req.ID)
	}

	req = pq.Pop()
	if req.ID != "old-low" {
		t.Errorf("expected 'old-low', got '%s'", req.ID)
	}
}

func TestPriorityQueue_Len(t *testing.T) {
	pq := NewPriorityQueue()

	if pq.Len() != 0 {
		t.Errorf("expected len 0, got %d", pq.Len())
	}

	pq.Push(&Request{ID: "1", Priority: 1, SubmitTime: time.Now()})
	pq.Push(&Request{ID: "2", Priority: 1, SubmitTime: time.Now()})

	if pq.Len() != 2 {
		t.Errorf("expected len 2, got %d", pq.Len())
	}

	pq.Pop()
	if pq.Len() != 1 {
		t.Errorf("expected len 1, got %d", pq.Len())
	}
}

func TestPriorityQueue_BlockingPop(t *testing.T) {
	pq := NewPriorityQueue()

	done := make(chan bool)

	// Start a goroutine that will block on Pop
	go func() {
		req := pq.Pop()
		if req.ID != "delayed" {
			t.Errorf("expected 'delayed', got '%s'", req.ID)
		}
		done <- true
	}()

	// Give the goroutine time to block
	time.Sleep(50 * time.Millisecond)

	// Push should unblock the waiting goroutine
	pq.Push(&Request{ID: "delayed", Priority: 1, SubmitTime: time.Now()})

	select {
	case <-done:
		// Success
	case <-time.After(time.Second):
		t.Error("Pop did not unblock after Push")
	}
}

func TestPriorityQueue_ConcurrentPush(t *testing.T) {
	pq := NewPriorityQueue()
	numProducers := 5
	itemsPerProducer := 100

	var wg sync.WaitGroup

	// Start producers concurrently
	for i := 0; i < numProducers; i++ {
		wg.Add(1)
		go func(producerID int) {
			defer wg.Done()
			for j := 0; j < itemsPerProducer; j++ {
				pq.Push(&Request{
					ID:         string(rune('A'+producerID)) + "-" + string(rune('0'+j%10)),
					Priority:   j % 10,
					SubmitTime: time.Now(),
				})
			}
		}(i)
	}

	wg.Wait()

	expected := numProducers * itemsPerProducer
	if pq.Len() != expected {
		t.Errorf("expected %d items in queue, got %d", expected, pq.Len())
	}

	// Verify we can pop all items and they come out in priority order
	var lastPriority int = 100
	for i := 0; i < expected; i++ {
		req := pq.Pop()
		if req.Priority > lastPriority {
			t.Errorf("priority order violated: got %d after %d", req.Priority, lastPriority)
		}
		lastPriority = req.Priority
	}
}

func TestPriorityQueue_MultipleBlockingConsumers(t *testing.T) {
	pq := NewPriorityQueue()
	numConsumers := 3
	numItems := numConsumers // Exactly one item per consumer

	results := make(chan string, numItems)
	var wg sync.WaitGroup

	// Push items first
	for i := 0; i < numItems; i++ {
		pq.Push(&Request{
			ID:         string(rune('A' + i)),
			Priority:   i,
			SubmitTime: time.Now(),
		})
	}

	// Start consumers - each will grab exactly one item
	for i := 0; i < numConsumers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req := pq.Pop() // Blocking pop
			results <- req.ID
		}()
	}

	wg.Wait()
	close(results)

	count := 0
	for range results {
		count++
	}

	if count != numItems {
		t.Errorf("expected %d items processed, got %d", numItems, count)
	}

	if pq.Len() != 0 {
		t.Errorf("expected queue to be empty, got %d items", pq.Len())
	}
}
