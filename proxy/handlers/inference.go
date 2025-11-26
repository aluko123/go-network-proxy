package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	pb "github.com/aluko123/go-network-proxy/inference/pb"
	"github.com/aluko123/go-network-proxy/inference/queue"
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
	// 1. Parse request
	var reqBody struct {
		Prompt      string  `json:"prompt"`
		MaxTokens   int     `json:"max_tokens"`
		Temperature float32 `json:"temperature"`
		Model       string  `json:"model"`
		Priority    int     `json:"priority"` // Optional: Let users set priority (or derive from API key)
	}

	if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	// Apply Defaults
	if reqBody.Temperature <= 0 {
		reqBody.Temperature = 0.7
	}
	if reqBody.MaxTokens <= 0 {
		reqBody.MaxTokens = 100
	}
	if reqBody.Model == "" {
		reqBody.Model = "default-model"
	}
	if reqBody.Priority <= 0 {
		reqBody.Priority = 1 // Default low priority
	}
	if reqBody.Prompt == "" {
		http.Error(w, "Prompt is required", http.StatusBadRequest)
		return
	}

	// 2. Create Internal Request
	req := &queue.Request{
		ID:          fmt.Sprintf("req-%d", time.Now().UnixNano()),
		Prompt:      reqBody.Prompt,
		MaxTokens:   reqBody.MaxTokens,
		Temperature: reqBody.Temperature,
		Model:       reqBody.Model,
		Priority:    reqBody.Priority,
		SubmitTime:  time.Now(),
		ResponseCh:  make(chan *pb.TokenResponse, 100), // Buffered to avoid blocking worker
		ErrorCh:     make(chan error, 1),
	}

	// 3. Enqueue (This is non-blocking usually, but we can measure queue time here)
	h.queue.Push(req)

	// 4. Stream Response
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
		return
	}

	for {
		select {
		case resp, ok := <-req.ResponseCh:
			if !ok {
				return // Channel closed (success)
			}
			// SSE Format: data: <token>\n\n
			// We send JSON to be cleaner
			data, _ := json.Marshal(resp)
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()

			if resp.Finished {
				return
			}

		case err := <-req.ErrorCh:
			// If headers sent, we can only write to stream
			fmt.Fprintf(w, "event: error\ndata: %s\n\n", err.Error())
			return

		case <-r.Context().Done():
			// Client disconnected
			return
		}
	}
}
