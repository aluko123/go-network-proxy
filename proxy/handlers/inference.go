package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/aluko123/go-network-proxy/inference/backend"
	"github.com/aluko123/go-network-proxy/inference/queue"
	"github.com/aluko123/go-network-proxy/pkg/logger"
	"github.com/aluko123/go-network-proxy/pkg/metrics"
)

type InferenceHandler struct {
	queue *queue.PriorityQueue
}

func NewInferenceHandler(pq *queue.PriorityQueue) *InferenceHandler {
	return &InferenceHandler{
		queue: pq,
	}
}

func (h *InferenceHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var reqBody struct {
		Prompt      string  `json:"prompt"`
		Prefix      string  `json:"prefix"` // Optional: cacheable prefix for KV cache affinity
		MaxTokens   int     `json:"max_tokens"`
		Temperature float32 `json:"temperature"`
		Model       string  `json:"model"`
		Priority    int     `json:"priority"`
	}

	if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	if reqBody.Temperature <= 0 {
		reqBody.Temperature = 0.7
	}
	if reqBody.MaxTokens <= 0 {
		reqBody.MaxTokens = 100
	}
	if reqBody.Model == "" {
		reqBody.Model = "default"
	}
	if reqBody.Priority <= 0 {
		reqBody.Priority = 1
	}
	if reqBody.Prompt == "" {
		http.Error(w, "Prompt is required", http.StatusBadRequest)
		return
	}

	reqID, ok := r.Context().Value(logger.RequestIDKey).(string)
	if !ok {
		reqID = fmt.Sprintf("req-%d", time.Now().UnixNano())
	}

	req := &queue.Request{
		ID:          reqID,
		Prompt:      reqBody.Prompt,
		Prefix:      reqBody.Prefix,
		MaxTokens:   reqBody.MaxTokens,
		Temperature: reqBody.Temperature,
		Model:       reqBody.Model,
		Priority:    reqBody.Priority,
		SubmitTime:  time.Now(),
		Context:     r.Context(),
		ResponseCh:  make(chan any, 100),
		ErrorCh:     make(chan error, 1),
	}

	if !h.queue.Push(req) {
		http.Error(w, "Service shutting down", http.StatusServiceUnavailable)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
		return
	}

	var firstTokenReceived bool
	var lastTokenCount int

	defer func() {
		metrics.InferenceRequestDuration.WithLabelValues(req.Model).Observe(time.Since(req.SubmitTime).Seconds())
	}()

	for {
		select {
		case resp, ok := <-req.ResponseCh:
			if !ok {
				return
			}

			token, isToken := resp.(backend.Token)
			if !isToken {
				continue
			}

			if !firstTokenReceived && token.Text != "" {
				firstTokenReceived = true
				metrics.InferenceTimeToFirstToken.WithLabelValues(req.Model).Observe(time.Since(req.SubmitTime).Seconds())
			}

			if token.TokenCount > lastTokenCount {
				metrics.InferenceTokensTotal.WithLabelValues(req.Model).Add(float64(token.TokenCount - lastTokenCount))
				lastTokenCount = token.TokenCount
			}

			sseData := map[string]any{
				"request_id":  token.RequestID,
				"token":       token.Text,
				"token_count": token.TokenCount,
				"finished":    token.Finished,
			}
			data, _ := json.Marshal(sseData)
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()

			if token.Finished {
				return
			}

		case err := <-req.ErrorCh:
			fmt.Fprintf(w, "event: error\ndata: %s\n\n", err.Error())
			flusher.Flush()
			return

		case <-r.Context().Done():
			return
		}
	}
}
