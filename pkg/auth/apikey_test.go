package auth

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNewKeyStore(t *testing.T) {
	ks := NewKeyStore()
	if ks == nil {
		t.Fatal("NewKeyStore returned nil")
	}
	if ks.Count() != 0 {
		t.Errorf("expected empty keystore, got %d keys", ks.Count())
	}
}

func TestKeyStore_Validate_Empty(t *testing.T) {
	ks := NewKeyStore()
	_, valid := ks.Validate("any-token")
	if valid {
		t.Error("expected invalid for empty keystore")
	}
}

func TestKeyStore_Validate_ValidKey(t *testing.T) {
	ks := NewKeyStore()
	ks.keys["sk-test-123"] = KeyInfo{Name: "test-key", Key: "sk-test-123"}

	info, valid := ks.Validate("sk-test-123")
	if !valid {
		t.Error("expected valid for correct key")
	}
	if info.Name != "test-key" {
		t.Errorf("expected name 'test-key', got '%s'", info.Name)
	}
}

func TestKeyStore_Validate_InvalidKey(t *testing.T) {
	ks := NewKeyStore()
	ks.keys["sk-test-123"] = KeyInfo{Name: "test-key", Key: "sk-test-123"}

	_, valid := ks.Validate("sk-wrong-key")
	if valid {
		t.Error("expected invalid for wrong key")
	}
}

func TestKeyStore_Validate_TimingAttackResistance(t *testing.T) {
	ks := NewKeyStore()
	correctKey := "sk-correct-key-12345678901234567890"
	ks.keys[correctKey] = KeyInfo{Name: "test", Key: correctKey}

	// These should all take similar time due to constant-time compare
	testKeys := []string{
		"sk-wrong-key-12345678901234567890",   // same length, different
		"sk-correct-key-12345678901234567890", // correct
		"short",                                // different length
		"sk-correct-key-1234567890123456789",  // off by one char
	}

	for _, key := range testKeys {
		_, _ = ks.Validate(key)
	}
	// Note: Actually testing timing would require statistical analysis
	// This just ensures the code path works
}

func TestKeyStore_LoadFromFile(t *testing.T) {
	// Create temp file
	content := `{
		"keys": [
			{"name": "key1", "key": "sk-key-111"},
			{"name": "key2", "key": "sk-key-222"}
		]
	}`
	
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "keys.json")
	if err := os.WriteFile(tmpFile, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write temp file: %v", err)
	}

	ks := NewKeyStore()
	if err := ks.LoadFromFile(tmpFile); err != nil {
		t.Fatalf("LoadFromFile failed: %v", err)
	}

	if ks.Count() != 2 {
		t.Errorf("expected 2 keys, got %d", ks.Count())
	}

	// Validate both keys work
	if _, valid := ks.Validate("sk-key-111"); !valid {
		t.Error("key1 should be valid")
	}
	if _, valid := ks.Validate("sk-key-222"); !valid {
		t.Error("key2 should be valid")
	}
}

func TestKeyStore_LoadFromFile_NotFound(t *testing.T) {
	ks := NewKeyStore()
	err := ks.LoadFromFile("/nonexistent/path/keys.json")
	if err == nil {
		t.Error("expected error for nonexistent file")
	}
}

func TestKeyStore_LoadFromFile_InvalidJSON(t *testing.T) {
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "invalid.json")
	if err := os.WriteFile(tmpFile, []byte("not valid json"), 0644); err != nil {
		t.Fatalf("failed to write temp file: %v", err)
	}

	ks := NewKeyStore()
	err := ks.LoadFromFile(tmpFile)
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestKeyStore_Count(t *testing.T) {
	ks := NewKeyStore()
	
	if ks.Count() != 0 {
		t.Errorf("expected 0, got %d", ks.Count())
	}

	ks.keys["key1"] = KeyInfo{Name: "k1", Key: "key1"}
	if ks.Count() != 1 {
		t.Errorf("expected 1, got %d", ks.Count())
	}

	ks.keys["key2"] = KeyInfo{Name: "k2", Key: "key2"}
	if ks.Count() != 2 {
		t.Errorf("expected 2, got %d", ks.Count())
	}
}

func TestKeyStore_Validate_EmptyToken(t *testing.T) {
	ks := NewKeyStore()
	ks.keys["sk-valid"] = KeyInfo{Name: "test", Key: "sk-valid"}

	_, valid := ks.Validate("")
	if valid {
		t.Error("empty token should not be valid")
	}
}
