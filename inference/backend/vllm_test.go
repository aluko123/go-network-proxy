package backend

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestVLLMBackend_Generate(t *testing.T) {
	// Create mock vLLM server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			json.NewEncoder(w).Encode(map[string]any{
				"status":           "healthy",
				"gpu_memory_used":  10000000000,
				"gpu_memory_total": 24000000000,
				"kv_cache_usage":   0.3,
			})
			return
		}

		if r.URL.Path == "/v1/chat/completions" {
			var req map[string]any
			json.NewDecoder(r.Body).Decode(&req)

			// Verify request structure
			messages, ok := req["messages"].([]any)
			if !ok || len(messages) == 0 {
				t.Error("expected messages in request")
			}

			// Stream response
			w.Header().Set("Content-Type", "text/event-stream")
			flusher := w.(http.Flusher)

			tokens := []string{"Hello", "world", "from", "vLLM"}
			for i, token := range tokens {
				chunk := map[string]any{
					"id":     "test-123",
					"object": "chat.completion.chunk",
					"model":  "test-model",
					"choices": []map[string]any{{
						"index":         0,
						"delta":         map[string]string{"content": token + " "},
						"finish_reason": nil,
					}},
				}
				data, _ := json.Marshal(chunk)
				fmt.Fprintf(w, "data: %s\n\n", data)
				flusher.Flush()
				_ = i
			}

			// Final chunk
			finalChunk := map[string]any{
				"id":     "test-123",
				"object": "chat.completion.chunk",
				"model":  "test-model",
				"choices": []map[string]any{{
					"index":         0,
					"delta":         map[string]string{},
					"finish_reason": "stop",
				}},
			}
			data, _ := json.Marshal(finalChunk)
			fmt.Fprintf(w, "data: %s\n\n", data)
			fmt.Fprintf(w, "data: [DONE]\n\n")
			flusher.Flush()
		}
	}))
	defer server.Close()

	backend := NewVLLMBackend(VLLMConfig{
		Name:    "test-vllm",
		BaseURL: server.URL,
		Models:  []string{"test-model"},
	})

	req := &Request{
		ID:          "test-req",
		Model:       "test-model",
		Prompt:      "Hello",
		Prefix:      "You are helpful",
		MaxTokens:   50,
		Temperature: 0.7,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	tokenCh, err := backend.Generate(ctx, req)
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	var tokens []string
	for token := range tokenCh {
		if token.Error != nil {
			t.Fatalf("token error: %v", token.Error)
		}
		if token.Text != "" {
			tokens = append(tokens, strings.TrimSpace(token.Text))
		}
		if token.Finished {
			break
		}
	}

	if len(tokens) != 4 {
		t.Errorf("expected 4 tokens, got %d: %v", len(tokens), tokens)
	}

	expected := []string{"Hello", "world", "from", "vLLM"}
	for i, tok := range tokens {
		if tok != expected[i] {
			t.Errorf("token %d: expected %q, got %q", i, expected[i], tok)
		}
	}
}

func TestVLLMBackend_HealthCheck(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			json.NewEncoder(w).Encode(map[string]any{
				"status":            "healthy",
				"gpu_memory_used":   15000000000,
				"gpu_memory_total":  24000000000,
				"kv_cache_usage":    0.45,
				"requests_running":  3,
			})
		}
	}))
	defer server.Close()

	backend := NewVLLMBackend(VLLMConfig{
		Name:    "test-vllm",
		BaseURL: server.URL,
		Models:  []string{"test-model"},
	})

	ctx := context.Background()
	err := backend.CheckHealth(ctx)
	if err != nil {
		t.Fatalf("health check failed: %v", err)
	}

	if !backend.Healthy() {
		t.Error("backend should be healthy")
	}

	memUsed, memTotal, kvUsage := backend.GPUStats()
	if memUsed != 15000000000 {
		t.Errorf("expected memUsed 15000000000, got %d", memUsed)
	}
	if memTotal != 24000000000 {
		t.Errorf("expected memTotal 24000000000, got %d", memTotal)
	}
	if kvUsage != 0.45 {
		t.Errorf("expected kvUsage 0.45, got %f", kvUsage)
	}
}

func TestVLLMBackend_PrefixAsSystemMessage(t *testing.T) {
	var receivedMessages []map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/chat/completions" {
			var req map[string]any
			json.NewDecoder(r.Body).Decode(&req)

			messages, _ := req["messages"].([]any)
			for _, m := range messages {
				receivedMessages = append(receivedMessages, m.(map[string]any))
			}

			// Send minimal response
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprintf(w, "data: [DONE]\n\n")
		}
	}))
	defer server.Close()

	backend := NewVLLMBackend(VLLMConfig{
		Name:    "test-vllm",
		BaseURL: server.URL,
		Models:  []string{"test-model"},
	})

	req := &Request{
		ID:     "test-req",
		Model:  "test-model",
		Prompt: "What is 2+2?",
		Prefix: "You are a math tutor.",
	}

	ctx := context.Background()
	tokenCh, _ := backend.Generate(ctx, req)

	// Drain channel
	for range tokenCh {
	}

	// Verify messages structure
	if len(receivedMessages) != 2 {
		t.Fatalf("expected 2 messages (system + user), got %d", len(receivedMessages))
	}

	if receivedMessages[0]["role"] != "system" {
		t.Errorf("first message should be system, got %v", receivedMessages[0]["role"])
	}
	if receivedMessages[0]["content"] != "You are a math tutor." {
		t.Errorf("system message content wrong: %v", receivedMessages[0]["content"])
	}

	if receivedMessages[1]["role"] != "user" {
		t.Errorf("second message should be user, got %v", receivedMessages[1]["role"])
	}
	if receivedMessages[1]["content"] != "What is 2+2?" {
		t.Errorf("user message content wrong: %v", receivedMessages[1]["content"])
	}
}
