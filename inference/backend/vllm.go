package backend

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"
)

type VLLMBackend struct {
	mu      sync.RWMutex
	name    string
	baseURL string
	models  []string
	client  *http.Client
	healthy bool

	// GPU stats from health endpoint
	gpuMemoryUsed   int64
	gpuMemoryTotal  int64
	kvCacheUsage    float32
	requestsRunning int
}

type VLLMConfig struct {
	Name    string
	BaseURL string
	Models  []string
	Timeout time.Duration
}

func NewVLLMBackend(cfg VLLMConfig) *VLLMBackend {
	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = 5 * time.Minute
	}

	return &VLLMBackend{
		name:    cfg.Name,
		baseURL: strings.TrimSuffix(cfg.BaseURL, "/"),
		models:  cfg.Models,
		client: &http.Client{
			Timeout: timeout,
		},
		healthy: true,
	}
}

func (v *VLLMBackend) Name() string     { return v.name }
func (v *VLLMBackend) Type() string     { return "vllm" }
func (v *VLLMBackend) Models() []string { return v.models }
func (v *VLLMBackend) Close() error     { return nil }

func (v *VLLMBackend) Healthy() bool {
	v.mu.RLock()
	defer v.mu.RUnlock()
	return v.healthy
}

func (v *VLLMBackend) GPUStats() (memUsed, memTotal int64, kvUsage float32) {
	v.mu.RLock()
	defer v.mu.RUnlock()
	return v.gpuMemoryUsed, v.gpuMemoryTotal, v.kvCacheUsage
}

type vllmRequest struct {
	Model       string          `json:"model"`
	Messages    []vllmMessage   `json:"messages"`
	MaxTokens   int             `json:"max_tokens,omitempty"`
	Temperature float32         `json:"temperature,omitempty"`
	Stream      bool            `json:"stream"`
}

type vllmMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type vllmStreamChunk struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Choices []struct {
		Index int `json:"index"`
		Delta struct {
			Content string `json:"content"`
		} `json:"delta"`
		FinishReason *string `json:"finish_reason"`
	} `json:"choices"`
}

type vllmHealthResponse struct {
	Status          string  `json:"status"`
	GPUMemoryUsed   int64   `json:"gpu_memory_used,omitempty"`
	GPUMemoryTotal  int64   `json:"gpu_memory_total,omitempty"`
	KVCacheUsage    float32 `json:"kv_cache_usage,omitempty"`
	RequestsRunning int     `json:"requests_running,omitempty"`
}

func (v *VLLMBackend) Generate(ctx context.Context, req *Request) (<-chan Token, error) {
	tokenCh := make(chan Token, 100)

	go func() {
		defer close(tokenCh)

		// Build messages - if prefix provided, add as system message
		messages := make([]vllmMessage, 0, 2)
		if req.Prefix != "" {
			messages = append(messages, vllmMessage{
				Role:    "system",
				Content: req.Prefix,
			})
		}
		messages = append(messages, vllmMessage{
			Role:    "user",
			Content: req.Prompt,
		})

		vllmReq := vllmRequest{
			Model:       req.Model,
			Messages:    messages,
			MaxTokens:   req.MaxTokens,
			Temperature: req.Temperature,
			Stream:      true,
		}

		body, err := json.Marshal(vllmReq)
		if err != nil {
			tokenCh <- Token{RequestID: req.ID, Error: err, Finished: true}
			return
		}

		httpReq, err := http.NewRequestWithContext(ctx, "POST",
			v.baseURL+"/v1/chat/completions",
			bytes.NewReader(body))
		if err != nil {
			tokenCh <- Token{RequestID: req.ID, Error: err, Finished: true}
			return
		}

		httpReq.Header.Set("Content-Type", "application/json")
		httpReq.Header.Set("Accept", "text/event-stream")

		resp, err := v.client.Do(httpReq)
		if err != nil {
			v.setHealthy(false)
			tokenCh <- Token{RequestID: req.ID, Error: err, Finished: true}
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			bodyBytes, _ := io.ReadAll(resp.Body)
			tokenCh <- Token{
				RequestID: req.ID,
				Error:     fmt.Errorf("vLLM API error %d: %s", resp.StatusCode, string(bodyBytes)),
				Finished:  true,
			}
			return
		}

		v.setHealthy(true)
		tokenCount := 0

		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			line := scanner.Text()

			if !strings.HasPrefix(line, "data: ") {
				continue
			}

			data := strings.TrimPrefix(line, "data: ")
			if data == "[DONE]" {
				tokenCh <- Token{
					RequestID:  req.ID,
					TokenCount: tokenCount,
					Finished:   true,
				}
				return
			}

			var chunk vllmStreamChunk
			if err := json.Unmarshal([]byte(data), &chunk); err != nil {
				continue
			}

			if len(chunk.Choices) > 0 && chunk.Choices[0].Delta.Content != "" {
				tokenCount++
				tokenCh <- Token{
					RequestID:  req.ID,
					Text:       chunk.Choices[0].Delta.Content,
					TokenCount: tokenCount,
					Finished:   false,
				}
			}
		}

		if err := scanner.Err(); err != nil {
			tokenCh <- Token{RequestID: req.ID, Error: err, Finished: true}
		}
	}()

	return tokenCh, nil
}

func (v *VLLMBackend) setHealthy(healthy bool) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.healthy = healthy
}

func (v *VLLMBackend) CheckHealth(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, "GET", v.baseURL+"/health", nil)
	if err != nil {
		return err
	}

	resp, err := v.client.Do(req)
	if err != nil {
		v.setHealthy(false)
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		v.setHealthy(false)
		return fmt.Errorf("health check failed: %d", resp.StatusCode)
	}

	var health vllmHealthResponse
	if err := json.NewDecoder(resp.Body).Decode(&health); err != nil {
		// vLLM might return empty body on /health, that's OK
		v.setHealthy(true)
		return nil
	}

	v.mu.Lock()
	v.healthy = true
	v.gpuMemoryUsed = health.GPUMemoryUsed
	v.gpuMemoryTotal = health.GPUMemoryTotal
	v.kvCacheUsage = health.KVCacheUsage
	v.requestsRunning = health.RequestsRunning
	v.mu.Unlock()

	slog.Debug("vllm health check",
		"backend", v.name,
		"gpu_mem_used", health.GPUMemoryUsed,
		"gpu_mem_total", health.GPUMemoryTotal,
		"kv_cache_usage", health.KVCacheUsage,
	)

	return nil
}

func (v *VLLMBackend) StartHealthChecks(ctx context.Context, interval time.Duration) {
	if interval == 0 {
		interval = 10 * time.Second
	}

	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		// Initial check
		if err := v.CheckHealth(ctx); err != nil {
			slog.Warn("vllm health check failed", "backend", v.name, "error", err)
		}

		for {
			select {
			case <-ticker.C:
				if err := v.CheckHealth(ctx); err != nil {
					slog.Warn("vllm health check failed", "backend", v.name, "error", err)
				}
			case <-ctx.Done():
				return
			}
		}
	}()
}
