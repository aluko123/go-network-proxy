package backend

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
)

type Token struct {
	RequestID  string
	Text       string
	TokenCount int
	Finished   bool
	Error      error
}

type Request struct {
	ID          string
	Model       string
	Prompt      string
	MaxTokens   int
	Temperature float32
	Priority    int
}

type Backend interface {
	Name() string
	Type() string
	Models() []string
	Generate(ctx context.Context, req *Request) (<-chan Token, error)
	Healthy() bool
	Close() error
}

type Registry struct {
	mu            sync.RWMutex
	backends      []Backend
	modelToBackend map[string]Backend
	defaultBackend Backend
}

func NewRegistry() *Registry {
	return &Registry{
		backends:       make([]Backend, 0),
		modelToBackend: make(map[string]Backend),
	}
}

func (r *Registry) Register(b Backend) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.backends = append(r.backends, b)

	for _, model := range b.Models() {
		if existing, ok := r.modelToBackend[model]; ok {
			slog.Warn("model already registered, overwriting",
				"model", model,
				"old_backend", existing.Name(),
				"new_backend", b.Name())
		}
		r.modelToBackend[model] = b
		slog.Info("registered model", "model", model, "backend", b.Name())
	}

	if r.defaultBackend == nil {
		r.defaultBackend = b
	}
}

func (r *Registry) SetDefault(name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	for _, b := range r.backends {
		if b.Name() == name {
			r.defaultBackend = b
			return nil
		}
	}
	return fmt.Errorf("backend not found: %s", name)
}

func (r *Registry) Route(model string) (Backend, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if model == "" || model == "default" {
		if r.defaultBackend != nil {
			return r.defaultBackend, nil
		}
		return nil, fmt.Errorf("no default backend configured")
	}

	if b, ok := r.modelToBackend[model]; ok {
		return b, nil
	}

	return nil, fmt.Errorf("unknown model: %s", model)
}

func (r *Registry) ListModels() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	models := make([]string, 0, len(r.modelToBackend))
	for model := range r.modelToBackend {
		models = append(models, model)
	}
	return models
}

func (r *Registry) ListBackends() []Backend {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]Backend, len(r.backends))
	copy(result, r.backends)
	return result
}

func (r *Registry) Close() {
	r.mu.Lock()
	defer r.mu.Unlock()

	for _, b := range r.backends {
		if err := b.Close(); err != nil {
			slog.Error("error closing backend", "backend", b.Name(), "error", err)
		}
	}
}
