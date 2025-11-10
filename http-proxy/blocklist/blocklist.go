package blocklist

import (
	"encoding/json"
	"os"
	"strings"
	"sync"
)

// Manager manages domain blocking with efficient O(1) lookups
type Manager struct {
	exactDomains    map[string]bool // exact domain matches
	wildcardDomains []string        // wildcard patterns like *.ads.com
	mu              sync.RWMutex    // thread-safe concurrent access
}

// Config represents the JSON structure
type Config struct {
	BlockedDomains []string `json:"blocked_domains"`
}

// NewManager creates a new blocklist manager
func NewManager() *Manager {
	return &Manager{
		exactDomains:    make(map[string]bool),
		wildcardDomains: make([]string, 0),
	}
}

// LoadFromFile loads blocked domains from a JSON file
func (m *Manager) LoadFromFile(filepath string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	data, err := os.ReadFile(filepath)
	if err != nil {
		return err
	}

	var config Config
	if err := json.Unmarshal(data, &config); err != nil {
		return err
	}

	// Clear existing entries
	m.exactDomains = make(map[string]bool)
	m.wildcardDomains = make([]string, 0)

	// Populate blocklist
	for _, domain := range config.BlockedDomains {
		domain = strings.ToLower(strings.TrimSpace(domain))
		if strings.HasPrefix(domain, "*.") {
			// Wildcard domain
			m.wildcardDomains = append(m.wildcardDomains, domain[2:]) // remove "*."
		} else {
			// Exact match
			m.exactDomains[domain] = true
		}
	}

	return nil
}

// IsBlocked checks if a domain is blocked (O(1) for exact, O(k) for wildcards)
func (m *Manager) IsBlocked(domain string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	domain = strings.ToLower(strings.TrimSpace(domain))

	// Check exact match first (O(1))
	if m.exactDomains[domain] {
		return true
	}

	// Check wildcard patterns (O(k) where k = number of wildcards)
	for _, wildcardDomain := range m.wildcardDomains {
		if strings.HasSuffix(domain, wildcardDomain) {
			return true
		}
	}

	return false
}

// GetBlockedResponse returns a custom blocked page response
func GetBlockedResponse() string {
	return `<!DOCTYPE html>
<html>
<head>
    <title>Domain Blocked</title>
    <style>
        body { font-family: Arial, sans-serif; text-align: center; padding: 50px; background: #f5f5f5; }
        .container { background: white; padding: 40px; border-radius: 10px; box-shadow: 0 2px 10px rgba(0,0,0,0.1); max-width: 600px; margin: 0 auto; }
        h1 { color: #e74c3c; }
        p { color: #555; }
    </style>
</head>
<body>
    <div class="container">
        <h1>🚫 Domain Blocked</h1>
        <p>Access to this domain has been blocked by network policy.</p>
        <p>If you believe this is an error, please contact your network administrator.</p>
    </div>
</body>
</html>`
}
