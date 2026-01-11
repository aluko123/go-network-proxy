package backend

import (
	"testing"
	"time"
)

func TestHashPrefix(t *testing.T) {
	hash1 := HashPrefix("llama-70b", "You are a helpful assistant")
	hash2 := HashPrefix("llama-70b", "You are a helpful assistant")
	hash3 := HashPrefix("llama-7b", "You are a helpful assistant")
	hash4 := HashPrefix("llama-70b", "You are a coding assistant")

	if hash1 != hash2 {
		t.Error("same model+prefix should produce same hash")
	}
	if hash1 == hash3 {
		t.Error("different model should produce different hash")
	}
	if hash1 == hash4 {
		t.Error("different prefix should produce different hash")
	}
	if len(hash1) != 16 {
		t.Errorf("hash should be 16 chars, got %d", len(hash1))
	}
}

func TestPrefixIndex_RecordAndLookup(t *testing.T) {
	idx := NewPrefixIndex(PrefixIndexConfig{TTL: 1 * time.Minute})

	prefixHash := HashPrefix("llama-70b", "You are a helpful assistant")

	workers := idx.Lookup(prefixHash)
	if len(workers) != 0 {
		t.Error("should return empty for unknown prefix")
	}

	idx.Record(prefixHash, "worker-1")
	idx.Record(prefixHash, "worker-2")

	workers = idx.Lookup(prefixHash)
	if len(workers) != 2 {
		t.Errorf("expected 2 workers, got %d", len(workers))
	}

	if !idx.HasWorker(prefixHash, "worker-1") {
		t.Error("should have worker-1")
	}
	if !idx.HasWorker(prefixHash, "worker-2") {
		t.Error("should have worker-2")
	}
	if idx.HasWorker(prefixHash, "worker-3") {
		t.Error("should not have worker-3")
	}
}

func TestPrefixIndex_TTLExpiry(t *testing.T) {
	idx := NewPrefixIndex(PrefixIndexConfig{TTL: 50 * time.Millisecond})

	prefixHash := HashPrefix("llama-70b", "test prefix")
	idx.Record(prefixHash, "worker-1")

	if !idx.HasWorker(prefixHash, "worker-1") {
		t.Error("should have worker-1 immediately after recording")
	}

	time.Sleep(100 * time.Millisecond)

	if idx.HasWorker(prefixHash, "worker-1") {
		t.Error("worker-1 should have expired after TTL")
	}

	workers := idx.Lookup(prefixHash)
	if len(workers) != 0 {
		t.Error("expired workers should not be returned")
	}
}

func TestPrefixIndex_RefreshTTL(t *testing.T) {
	idx := NewPrefixIndex(PrefixIndexConfig{TTL: 100 * time.Millisecond})

	prefixHash := HashPrefix("llama-70b", "test prefix")
	idx.Record(prefixHash, "worker-1")

	time.Sleep(60 * time.Millisecond)
	idx.Record(prefixHash, "worker-1")

	time.Sleep(60 * time.Millisecond)

	if !idx.HasWorker(prefixHash, "worker-1") {
		t.Error("worker-1 should still be valid after refresh")
	}
}

func TestPrefixIndex_Stats(t *testing.T) {
	idx := NewPrefixIndex(PrefixIndexConfig{TTL: 1 * time.Minute})

	prefixCount, workerMappings := idx.Stats()
	if prefixCount != 0 || workerMappings != 0 {
		t.Error("empty index should have zero stats")
	}

	hash1 := HashPrefix("llama-70b", "prefix 1")
	hash2 := HashPrefix("llama-70b", "prefix 2")

	idx.Record(hash1, "worker-1")
	idx.Record(hash1, "worker-2")
	idx.Record(hash2, "worker-1")

	prefixCount, workerMappings = idx.Stats()
	if prefixCount != 2 {
		t.Errorf("expected 2 prefixes, got %d", prefixCount)
	}
	if workerMappings != 3 {
		t.Errorf("expected 3 worker mappings, got %d", workerMappings)
	}
}

func TestPrefixIndex_MultipleModels(t *testing.T) {
	idx := NewPrefixIndex(PrefixIndexConfig{TTL: 1 * time.Minute})

	samePrefix := "You are a helpful assistant"
	hash1 := HashPrefix("llama-70b", samePrefix)
	hash2 := HashPrefix("mistral-7b", samePrefix)

	idx.Record(hash1, "gpu-worker-1")
	idx.Record(hash2, "gpu-worker-2")

	if !idx.HasWorker(hash1, "gpu-worker-1") {
		t.Error("llama model should route to gpu-worker-1")
	}
	if idx.HasWorker(hash1, "gpu-worker-2") {
		t.Error("llama model should NOT route to gpu-worker-2")
	}
	if !idx.HasWorker(hash2, "gpu-worker-2") {
		t.Error("mistral model should route to gpu-worker-2")
	}
}
