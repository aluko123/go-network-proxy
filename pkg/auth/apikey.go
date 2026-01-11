package auth

import (
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"os"
	"sync"
)

type KeyStore struct {
	mu   sync.RWMutex
	keys map[string]KeyInfo
}

type KeyInfo struct {
	Name string `json:"name"`
	Key  string `json:"key"`
}

type keysFile struct {
	Keys []KeyInfo `json:"keys"`
}

func NewKeyStore() *KeyStore {
	return &KeyStore{
		keys: make(map[string]KeyInfo),
	}
}

func (ks *KeyStore) LoadFromFile(path string) error {
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open keys file: %w", err)
	}
	defer file.Close()

	var kf keysFile
	if err := json.NewDecoder(file).Decode(&kf); err != nil {
		return fmt.Errorf("decode keys file: %w", err)
	}

	ks.mu.Lock()
	defer ks.mu.Unlock()

	for _, k := range kf.Keys {
		ks.keys[k.Key] = k
	}

	return nil
}

func (ks *KeyStore) Validate(token string) (KeyInfo, bool) {
	ks.mu.RLock()
	defer ks.mu.RUnlock()

	for key, info := range ks.keys {
		if subtle.ConstantTimeCompare([]byte(token), []byte(key)) == 1 {
			return info, true
		}
	}
	return KeyInfo{}, false
}

func (ks *KeyStore) Count() int {
	ks.mu.RLock()
	defer ks.mu.RUnlock()
	return len(ks.keys)
}

func (ks *KeyStore) Add(key, name string) {
	ks.mu.Lock()
	defer ks.mu.Unlock()
	ks.keys[key] = KeyInfo{Key: key, Name: name}
}
