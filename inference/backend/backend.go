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
	Prefix      string  // Cacheable prefix (system prompt, etc.) for KV cache affinity routing
	MaxTokens   int
	Temperature float32
	Priority    int

	// Distributed inference requirements
	RequiresDistributed bool // If true, only route to workers with active InfiniBand
	TensorParallel      int  // Number of GPUs for tensor parallelism (0 = auto)
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
	modelToBackend map[string][]Backend
	defaultBackend Backend
}

func NewRegistry() *Registry {
	return &Registry{
		backends:       make([]Backend, 0),
		modelToBackend: make(map[string][]Backend),
	}
}

func (r *Registry) Register(b Backend) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.backends = append(r.backends, b)

	for _, model := range b.Models() {
		r.modelToBackend[model] = append(r.modelToBackend[model], b)
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

	if backends, ok := r.modelToBackend[model]; ok && len(backends) > 0 {
		return backends[0], nil
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

func (r *Registry) RouteAll(model string) ([]Backend, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if model == "" || model == "default" {
		if r.defaultBackend != nil {
			return []Backend{r.defaultBackend}, nil
		}
		return nil, fmt.Errorf("no default backend configured")
	}

	if backends, ok := r.modelToBackend[model]; ok && len(backends) > 0 {
		return append([]Backend(nil), backends...), nil
	}

	return nil, fmt.Errorf("unknown model: %s", model)
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
