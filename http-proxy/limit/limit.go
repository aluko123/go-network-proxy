package limit

import (
	"log"
	"net"
	"net/http"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// MemoryRateLimiter tracks rate limiters per IP
type MemoryRateLimiter struct {
	limiters map[string]*rate.Limiter
	mu       sync.RWMutex
	r        rate.Limit // requests per second
	b        int        // burst size
	done     chan struct{}
}

// NewMemoryRateLimiter creates a new IP-based rate limiter
// r: requests per second (e.g., 100 = 100 req/s)
// b: burst size (e.g., 10 = allow 10 requests immediately)
func NewMemoryRateLimiter(r rate.Limit, b int) *MemoryRateLimiter {
	m := &MemoryRateLimiter{
		limiters: make(map[string]*rate.Limiter),
		r:        r,
		b:        b,
		done:     make(chan struct{}),
	}

	go m.cleanupLoop()

	return m
}

// GetLimiter returns the rate limiter for the given IP
func (m *MemoryRateLimiter) GetLimiter(ip string) *rate.Limiter {
	m.mu.Lock()
	defer m.mu.Unlock()

	limiter, exists := m.limiters[ip]
	if !exists {
		limiter = rate.NewLimiter(m.r, m.b)
		m.limiters[ip] = limiter
	}

	return limiter
}

func (m *MemoryRateLimiter) Allow(ip string) bool {
	limiter := m.GetLimiter(ip)
	return limiter.Allow()
}

func (m *MemoryRateLimiter) cleanupLoop() {

	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			m.cleanup()
		case <-m.done:
			return
		}
	}
}

// CleanupStale removes limiters that haven't been used in the given duration
func (m *MemoryRateLimiter) cleanup() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.limiters = make(map[string]*rate.Limiter)
	log.Println("Cleaned up stale rate limiters")
}

func (m *MemoryRateLimiter) Close() error {
	close(m.done)
	return nil
}

// GetIP extracts the client IP from the request
func GetIP(r *http.Request) string {
	// Try X-Forwarded-For first (if behind load balancer)
	forwarded := r.Header.Get("X-Forwarded-For")
	if forwarded != "" {
		// Take first IP in the list
		ip, _, _ := net.SplitHostPort(forwarded)
		if ip != "" {
			return ip
		}
		return forwarded
	}

	// Try X-Real-IP
	realIP := r.Header.Get("X-Real-IP")
	if realIP != "" {
		return realIP
	}

	// Fall back to RemoteAddr
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return ip
}

// Middleware returns a middleware that rate limits by IP
func (i *MemoryRateLimiter) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := GetIP(r)
		limiter := i.GetLimiter(ip)

		if !limiter.Allow() {
			http.Error(w, "Rate limit exceeded", http.StatusTooManyRequests)
			return
		}

		next.ServeHTTP(w, r)
	})
}
