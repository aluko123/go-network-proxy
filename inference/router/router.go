package router

import (
	"context"
	"log/slog"
	"time"

	"github.com/aluko123/go-network-proxy/inference/backend"
	"github.com/aluko123/go-network-proxy/inference/queue"
	"github.com/aluko123/go-network-proxy/pkg/metrics"
)

type Router struct {
	registry *backend.Registry
	queue    *queue.PriorityQueue
	workers  int
}

func NewRouter(registry *backend.Registry, pq *queue.PriorityQueue, workers int) *Router {
	if workers <= 0 {
		workers = 10
	}
	return &Router{
		registry: registry,
		queue:    pq,
		workers:  workers,
	}
}

func (r *Router) Start() {
	for i := 0; i < r.workers; i++ {
		go r.workerLoop(i)
	}
	slog.Info("router started", "workers", r.workers)
}

func (r *Router) workerLoop(id int) {
	slog.Info("starting router worker", "worker_id", id)
	for {
		req := r.queue.Pop()
		if req == nil {
			slog.Info("router worker stopping", "worker_id", id)
			return
		}

		r.processRequest(req)
		r.queue.Done()
	}
}

func (r *Router) processRequest(req *queue.Request) {
	req.StartTime = time.Now()
	priorityLabel := metrics.PriorityLabel(req.Priority)
	metrics.InferenceQueueWaitDuration.WithLabelValues(req.Model, priorityLabel).Observe(
		req.StartTime.Sub(req.SubmitTime).Seconds(),
	)

	b, err := r.registry.Route(req.Model)
	if err != nil {
		slog.Error("routing failed", "model", req.Model, "error", err)
		req.ErrorCh <- err
		metrics.InferenceRequestsTotal.WithLabelValues(req.Model, priorityLabel, "error").Inc()
		return
	}

	slog.Info("routing request",
		"request_id", req.ID,
		"model", req.Model,
		"backend", b.Name(),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	tokenCh, err := b.Generate(ctx, &backend.Request{
		ID:          req.ID,
		Model:       req.Model,
		Prompt:      req.Prompt,
		Prefix:      req.Prefix,
		MaxTokens:   req.MaxTokens,
		Temperature: req.Temperature,
		Priority:    req.Priority,
	})
	if err != nil {
		slog.Error("generate failed", "backend", b.Name(), "error", err)
		req.ErrorCh <- err
		metrics.InferenceRequestsTotal.WithLabelValues(req.Model, priorityLabel, "error").Inc()
		return
	}

	tokenCount := 0
	status := "success"

	for token := range tokenCh {
		if token.Error != nil {
			slog.Error("token error", "backend", b.Name(), "error", token.Error)
			req.ErrorCh <- token.Error
			status = "error"
			break
		}

		tokenCount++
		req.ResponseCh <- token

		if token.Finished {
			break
		}
	}

	close(req.ResponseCh)

	duration := time.Since(req.StartTime).Seconds()
	metrics.InferenceProcessingDuration.WithLabelValues(req.Model, b.Name()).Observe(duration)
	metrics.InferenceRequestsTotal.WithLabelValues(req.Model, priorityLabel, status).Inc()
	metrics.InferenceTokensTotal.WithLabelValues(req.Model).Add(float64(tokenCount))

	slog.Info("request completed",
		"request_id", req.ID,
		"model", req.Model,
		"backend", b.Name(),
		"tokens", tokenCount,
		"duration_ms", int(duration*1000),
	)
}

func (r *Router) Close() {
	r.queue.Close()
	r.queue.Wait()
	r.registry.Close()
	slog.Info("router stopped")
}

func (r *Router) Registry() *backend.Registry {
	return r.registry
}
