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

type OpenAIBackend struct {
	name    string
	apiKey  string
	baseURL string
	models  []string
	client  *http.Client
	healthy bool
}

type OpenAIConfig struct {
	Name    string
	APIKey  string
	BaseURL string
	Models  []string
	Timeout time.Duration
}

func NewOpenAIBackend(cfg OpenAIConfig) *OpenAIBackend {
	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = "https://api.openai.com"
	}

	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = 5 * time.Minute
	}

	return &OpenAIBackend{
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

func (o *OpenAIBackend) Name() string    { return o.name }
func (o *OpenAIBackend) Type() string    { return "openai" }
func (o *OpenAIBackend) Models() []string { return o.models }
func (o *OpenAIBackend) Healthy() bool   { return o.healthy }
func (o *OpenAIBackend) Close() error    { return nil }

type openAIRequest struct {
	Model       string          `json:"model"`
	Messages    []openAIMessage `json:"messages"`
	MaxTokens   int             `json:"max_tokens,omitempty"`
	Temperature float32         `json:"temperature,omitempty"`
	Stream      bool            `json:"stream"`
}

type openAIMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type openAIStreamChunk struct {
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

func (o *OpenAIBackend) Generate(ctx context.Context, req *Request) (<-chan Token, error) {
	tokenCh := make(chan Token, 100)

	go func() {
		defer close(tokenCh)

		oaiReq := openAIRequest{
			Model: req.Model,
			Messages: []openAIMessage{
				{Role: "user", Content: req.Prompt},
			},
			MaxTokens:   req.MaxTokens,
			Temperature: req.Temperature,
			Stream:      true,
		}

		body, err := json.Marshal(oaiReq)
		if err != nil {
			tokenCh <- Token{RequestID: req.ID, Error: err, Finished: true}
			return
		}

		httpReq, err := http.NewRequestWithContext(ctx, "POST",
			o.baseURL+"/v1/chat/completions",
			bytes.NewReader(body))
		if err != nil {
			tokenCh <- Token{RequestID: req.ID, Error: err, Finished: true}
			return
		}

		httpReq.Header.Set("Authorization", "Bearer "+o.apiKey)
		httpReq.Header.Set("Content-Type", "application/json")
		httpReq.Header.Set("Accept", "text/event-stream")

		resp, err := o.client.Do(httpReq)
		if err != nil {
			o.healthy = false
			tokenCh <- Token{RequestID: req.ID, Error: err, Finished: true}
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			bodyBytes, _ := io.ReadAll(resp.Body)
			tokenCh <- Token{
				RequestID: req.ID,
				Error:     fmt.Errorf("OpenAI API error %d: %s", resp.StatusCode, string(bodyBytes)),
				Finished:  true,
			}
			return
		}

		o.healthy = true
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

			var chunk openAIStreamChunk
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
