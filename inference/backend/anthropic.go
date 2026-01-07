package backend

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type AnthropicBackend struct {
	name    string
	apiKey  string
	baseURL string
	models  []string
	client  *http.Client
	healthy bool
}

type AnthropicConfig struct {
	Name    string
	APIKey  string
	BaseURL string
	Models  []string
	Timeout time.Duration
}

func NewAnthropicBackend(cfg AnthropicConfig) *AnthropicBackend {
	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = "https://api.anthropic.com"
	}

	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = 5 * time.Minute
	}

	return &AnthropicBackend{
		name:    cfg.Name,
		apiKey:  cfg.APIKey,
		baseURL: baseURL,
		models:  cfg.Models,
		client: &http.Client{
			Timeout: timeout,
		},
		healthy: true,
	}
}

func (a *AnthropicBackend) Name() string    { return a.name }
func (a *AnthropicBackend) Type() string    { return "anthropic" }
func (a *AnthropicBackend) Models() []string { return a.models }
func (a *AnthropicBackend) Healthy() bool   { return a.healthy }
func (a *AnthropicBackend) Close() error    { return nil }

type anthropicRequest struct {
	Model       string             `json:"model"`
	MaxTokens   int                `json:"max_tokens"`
	Messages    []anthropicMessage `json:"messages"`
	Temperature float32            `json:"temperature,omitempty"`
	Stream      bool               `json:"stream"`
}

type anthropicMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type anthropicStreamEvent struct {
	Type  string `json:"type"`
	Index int    `json:"index,omitempty"`
	Delta struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"delta,omitempty"`
	ContentBlock struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content_block,omitempty"`
	Message struct {
		Usage struct {
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
	} `json:"message,omitempty"`
}

func (a *AnthropicBackend) Generate(ctx context.Context, req *Request) (<-chan Token, error) {
	tokenCh := make(chan Token, 100)

	go func() {
		defer close(tokenCh)

		maxTokens := req.MaxTokens
		if maxTokens == 0 {
			maxTokens = 1024
		}

		antReq := anthropicRequest{
			Model:     req.Model,
			MaxTokens: maxTokens,
			Messages: []anthropicMessage{
				{Role: "user", Content: req.Prompt},
			},
			Temperature: req.Temperature,
			Stream:      true,
		}

		body, err := json.Marshal(antReq)
		if err != nil {
			tokenCh <- Token{RequestID: req.ID, Error: err, Finished: true}
			return
		}

		httpReq, err := http.NewRequestWithContext(ctx, "POST",
			a.baseURL+"/v1/messages",
			bytes.NewReader(body))
		if err != nil {
			tokenCh <- Token{RequestID: req.ID, Error: err, Finished: true}
			return
		}

		httpReq.Header.Set("x-api-key", a.apiKey)
		httpReq.Header.Set("anthropic-version", "2023-06-01")
		httpReq.Header.Set("Content-Type", "application/json")
		httpReq.Header.Set("Accept", "text/event-stream")

		resp, err := a.client.Do(httpReq)
		if err != nil {
			a.healthy = false
			tokenCh <- Token{RequestID: req.ID, Error: err, Finished: true}
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			bodyBytes, _ := io.ReadAll(resp.Body)
			tokenCh <- Token{
				RequestID: req.ID,
				Error:     fmt.Errorf("Anthropic API error %d: %s", resp.StatusCode, string(bodyBytes)),
				Finished:  true,
			}
			return
		}

		a.healthy = true
		tokenCount := 0

		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			line := scanner.Text()

			if strings.HasPrefix(line, "event: ") {
				continue
			}

			if !strings.HasPrefix(line, "data: ") {
				continue
			}

			data := strings.TrimPrefix(line, "data: ")

			var event anthropicStreamEvent
			if err := json.Unmarshal([]byte(data), &event); err != nil {
				continue
			}

			switch event.Type {
			case "content_block_delta":
				if event.Delta.Text != "" {
					tokenCount++
					tokenCh <- Token{
						RequestID:  req.ID,
						Text:       event.Delta.Text,
						TokenCount: tokenCount,
						Finished:   false,
					}
				}
			case "message_stop":
				tokenCh <- Token{
					RequestID:  req.ID,
					TokenCount: tokenCount,
					Finished:   true,
				}
				return
			case "message_delta":
				if event.Message.Usage.OutputTokens > 0 {
					tokenCount = event.Message.Usage.OutputTokens
				}
			}
		}

		if err := scanner.Err(); err != nil {
			tokenCh <- Token{RequestID: req.ID, Error: err, Finished: true}
		}
	}()

	return tokenCh, nil
}
