package backend

import (
	"crypto/sha256"
	"encoding/hex"
	"sync"
	"time"
)

type PrefixIndex struct {
	mu      sync.RWMutex
	entries map[string]*prefixEntry // prefixHash → entry
	ttl     time.Duration
}

type prefixEntry struct {
	workers   map[string]time.Time // workerAddress → lastSeen
	createdAt time.Time
}

type PrefixIndexConfig struct {
	TTL             time.Duration
	CleanupInterval time.Duration
}

func NewPrefixIndex(cfg PrefixIndexConfig) *PrefixIndex {
	if cfg.TTL == 0 {
		cfg.TTL = 60 * time.Second
	}
	if cfg.CleanupInterval == 0 {
		cfg.CleanupInterval = 30 * time.Second
	}

	idx := &PrefixIndex{
		entries: make(map[string]*prefixEntry),
		ttl:     cfg.TTL,
	}

	go idx.cleanupLoop(cfg.CleanupInterval)

	return idx
}

func HashPrefix(model, prefix string) string {
	h := sha256.New()
	h.Write([]byte(model))
	h.Write([]byte(":"))
	h.Write([]byte(prefix))
	return hex.EncodeToString(h.Sum(nil))[:16] // First 16 chars is enough
}

func (p *PrefixIndex) Record(prefixHash, workerAddress string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	entry, exists := p.entries[prefixHash]
	if !exists {
		entry = &prefixEntry{
			workers:   make(map[string]time.Time),
			createdAt: time.Now(),
		}
		p.entries[prefixHash] = entry
	}

	entry.workers[workerAddress] = time.Now()
}

func (p *PrefixIndex) Lookup(prefixHash string) []string {
	p.mu.RLock()
	defer p.mu.RUnlock()

	entry, exists := p.entries[prefixHash]
	if !exists {
		return nil
	}

	now := time.Now()
	workers := make([]string, 0, len(entry.workers))

	for addr, lastSeen := range entry.workers {
		if now.Sub(lastSeen) <= p.ttl {
			workers = append(workers, addr)
		}
	}

	return workers
}

func (p *PrefixIndex) HasWorker(prefixHash, workerAddress string) bool {
	p.mu.RLock()
	defer p.mu.RUnlock()

	entry, exists := p.entries[prefixHash]
	if !exists {
		return false
	}

	lastSeen, exists := entry.workers[workerAddress]
	if !exists {
		return false
	}

	return time.Since(lastSeen) <= p.ttl
}

func (p *PrefixIndex) cleanupLoop(interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for range ticker.C {
		p.cleanup()
	}
}

func (p *PrefixIndex) cleanup() {
	p.mu.Lock()
	defer p.mu.Unlock()

	now := time.Now()

	for prefixHash, entry := range p.entries {
		for addr, lastSeen := range entry.workers {
			if now.Sub(lastSeen) > p.ttl {
				delete(entry.workers, addr)
			}
		}
		if len(entry.workers) == 0 {
			delete(p.entries, prefixHash)
		}
	}
}

func (p *PrefixIndex) Stats() (prefixCount, workerMappings int) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	prefixCount = len(p.entries)
	for _, entry := range p.entries {
		workerMappings += len(entry.workers)
	}
	return
}
