package integration

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/aluko123/go-network-proxy/inference/backend"
	"github.com/aluko123/go-network-proxy/inference/queue"
	"github.com/aluko123/go-network-proxy/inference/router"
	"github.com/aluko123/go-network-proxy/pkg/auth"
	"github.com/aluko123/go-network-proxy/pkg/middleware"
	"github.com/aluko123/go-network-proxy/proxy/handlers"
)

func setupTestGateway(t *testing.T) (*httptest.Server, func()) {
	pq := queue.NewPriorityQueue()
	registry := backend.NewRegistry()

	mockBackend := &MockBackend{name: "test-backend", models: []string{"test-model", "default"}}
	registry.Register(mockBackend)

	r := router.NewRouter(registry, pq, 2)
	r.Start()

	handler := handlers.NewInferenceHandler(pq)

	apiKeys := auth.NewKeyStore()
	apiKeys.Add("test-key-123", "test-user")

	wrappedHandler := middleware.WithAPIKeyAuth(apiKeys)(handler)

	server := httptest.NewServer(wrappedHandler)

	cleanup := func() {
		server.Close()
		r.Close()
	}

	return server, cleanup
}

type MockBackend struct {
	name   string
	models []string
}

func (m *MockBackend) Name() string     { return m.name }
func (m *MockBackend) Type() string     { return "mock" }
func (m *MockBackend) Models() []string { return m.models }
func (m *MockBackend) Healthy() bool    { return true }
func (m *MockBackend) Close() error     { return nil }

func (m *MockBackend) Generate(ctx context.Context, req *backend.Request) (<-chan backend.Token, error) {
	ch := make(chan backend.Token, 10)

	go func() {
		defer close(ch)

		tokens := strings.Fields(req.Prompt)
		for i, token := range tokens {
			select {
			case <-ctx.Done():
				return
			case ch <- backend.Token{
				RequestID:  req.ID,
				Text:       token + " ",
				TokenCount: i + 1,
				Finished:   false,
			}:
			}
			time.Sleep(5 * time.Millisecond)
		}

		ch <- backend.Token{
			RequestID:  req.ID,
			TokenCount: len(tokens),
			Finished:   true,
		}
	}()

	return ch, nil
}

func TestInferenceEndToEnd(t *testing.T) {
	server, cleanup := setupTestGateway(t)
	defer cleanup()

	reqBody := map[string]any{
		"prompt":      "hello world test",
		"model":       "test-model",
		"max_tokens":  50,
		"temperature": 0.7,
		"priority":    5,
	}
	body, _ := json.Marshal(reqBody)

	req, err := http.NewRequest("POST", server.URL, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer test-key-123")
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var tokens []string
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "data:") {
			var data map[string]any
			if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &data); err != nil {
				continue
			}
			if token, ok := data["token"].(string); ok && token != "" {
				tokens = append(tokens, token)
			}
			if finished, ok := data["finished"].(bool); ok && finished {
				break
			}
		}
	}

	if len(tokens) == 0 {
		t.Error("expected tokens, got none")
	}
}

func TestInferenceWithPrefix(t *testing.T) {
	server, cleanup := setupTestGateway(t)
	defer cleanup()

	reqBody := map[string]any{
		"prompt":      "What is 2+2?",
		"prefix":      "You are a helpful math tutor.",
		"model":       "test-model",
		"max_tokens":  50,
		"temperature": 0.7,
		"priority":    5,
	}
	body, _ := json.Marshal(reqBody)

	req, err := http.NewRequest("POST", server.URL, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer test-key-123")
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var tokens []string
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "data:") {
			var data map[string]any
			if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &data); err != nil {
				continue
			}
			if token, ok := data["token"].(string); ok && token != "" {
				tokens = append(tokens, token)
			}
			if finished, ok := data["finished"].(bool); ok && finished {
				break
			}
		}
	}

	if len(tokens) == 0 {
		t.Error("expected tokens, got none")
	}
}

func TestAuthRequired(t *testing.T) {
	server, cleanup := setupTestGateway(t)
	defer cleanup()

	reqBody := map[string]any{"prompt": "test", "model": "test-model"}
	body, _ := json.Marshal(reqBody)

	req, err := http.NewRequest("POST", server.URL, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401 without auth, got %d", resp.StatusCode)
	}
}

func TestInvalidAPIKey(t *testing.T) {
	server, cleanup := setupTestGateway(t)
	defer cleanup()

	reqBody := map[string]any{"prompt": "test", "model": "test-model"}
	body, _ := json.Marshal(reqBody)

	req, err := http.NewRequest("POST", server.URL, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer wrong-key")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401 with invalid key, got %d", resp.StatusCode)
	}
}

func TestEmptyPromptRejected(t *testing.T) {
	server, cleanup := setupTestGateway(t)
	defer cleanup()

	reqBody := map[string]any{"prompt": "", "model": "test-model"}
	body, _ := json.Marshal(reqBody)

	req, err := http.NewRequest("POST", server.URL, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer test-key-123")
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400 for empty prompt, got %d", resp.StatusCode)
	}
}

func TestConcurrentRequests(t *testing.T) {
	server, cleanup := setupTestGateway(t)
	defer cleanup()

	const numRequests = 10
	results := make(chan error, numRequests)

	for i := 0; i < numRequests; i++ {
		go func(id int) {
			reqBody := map[string]any{
				"prompt":   "concurrent request test",
				"model":    "test-model",
				"priority": id % 10,
			}
			body, _ := json.Marshal(reqBody)

			req, _ := http.NewRequest("POST", server.URL, bytes.NewReader(body))
			req.Header.Set("Authorization", "Bearer test-key-123")
			req.Header.Set("Content-Type", "application/json")

			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				results <- err
				return
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				results <- nil
				return
			}

			scanner := bufio.NewScanner(resp.Body)
			for scanner.Scan() {
				line := scanner.Text()
				if strings.Contains(line, `"finished":true`) {
					break
				}
			}
			results <- nil
		}(i)
	}

	var errors []error
	for i := 0; i < numRequests; i++ {
		if err := <-results; err != nil {
			errors = append(errors, err)
		}
	}

	if len(errors) > 0 {
		t.Errorf("concurrent requests had %d errors: %v", len(errors), errors)
	}
}
