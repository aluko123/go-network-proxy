package main

import (
	"crypto/tls"
	"flag"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/aluko123/go-network-proxy/http-proxy/blocklist"
	"github.com/aluko123/go-network-proxy/http-proxy/handlers"
	"github.com/aluko123/go-network-proxy/http-proxy/limit"
	"github.com/aluko123/go-network-proxy/http-proxy/metrics"
	"github.com/aluko123/go-network-proxy/http-proxy/tunnel"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"golang.org/x/time/rate"
)

type statusRecorder struct {
	http.ResponseWriter
	statusCode  int
	wroteHeader bool
}

func (r *statusRecorder) WriteHeader(code int) {
	if r.wroteHeader {
		return
	}

	r.statusCode = code
	r.wroteHeader = true
	r.ResponseWriter.WriteHeader(code)
}

func main() {
	// Parse command-line flags
	var pemPath string
	flag.StringVar(&pemPath, "pem", "server.pem", "path to pem file")
	var keyPath string
	flag.StringVar(&keyPath, "key", "server.key", "path to key file")
	var proto string
	flag.StringVar(&proto, "proto", "http", "protocol to use: http or https")
	var debug bool
	flag.BoolVar(&debug, "debug", false, "enable debug logging")
	
	// Rate limiter configuration
	var limiterType string
	flag.StringVar(&limiterType, "limiter", "memory", "Rate limiter type: memory or redis")
	var redisAddr string
	flag.StringVar(&redisAddr, "redis-addr", "localhost:6379", "Redis server address")
	var rateLimit int
	flag.IntVar(&rateLimit, "rate-limit", 100, "Requests per minute per IP")
	var rateBurst int
	flag.IntVar(&rateBurst, "rate-burst", 20, "Burst size for rate limiter")
	
	flag.Parse()

	// Initialize blocklist manager once at startup
	bm := blocklist.NewManager()
	if err := bm.LoadFromFile("blocklist/blocklist.json"); err != nil {
		log.Printf("Warning: Could not load blocklist: %v", err)
	}

	// Initialize rate limiter based on flag
	var rateLimiter limit.RateLimiter
	var err error
	
	switch limiterType {
	case "redis":
		log.Printf("Initializing Redis rate limiter (addr: %s, limit: %d/min, burst: %d)", 
			redisAddr, rateLimit, rateBurst)
		rateLimiter, err = limit.NewRedisRateLimiter(redisAddr, rateLimit, time.Minute)
		if err != nil {
			log.Fatalf("Failed to initialize Redis rate limiter: %v", err)
		}
		log.Println("✓ Redis rate limiter initialized")
		
	case "memory":
		log.Printf("Initializing in-memory rate limiter (limit: %d/min, burst: %d)", 
			rateLimit, rateBurst)
		rateLimiter = limit.NewMemoryRateLimiter(rate.Limit(float64(rateLimit)/60), rateBurst)
		log.Println("✓ In-memory rate limiter initialized")
		
	default:
		log.Fatalf("Invalid limiter type: %s (must be 'memory' or 'redis')", limiterType)
	}
	
	defer rateLimiter.Close()

	// Configure server
	server := &http.Server{
		Addr: ":8080",
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Special case: metrics endpoint (skip rate limiting)
			if r.URL.Path == "/metrics" {
				promhttp.Handler().ServeHTTP(w, r)
				return
			}
			//log.Println("Testing Rate Limiter...")
			// Rate limiting check
			ip := limit.GetIP(r)
			//limiter := rateLimiter.GetLimiter(ip)

			// // DEBUG: check every 10th request
			// if limiter.Tokens() < 50 {
			// 	log.Printf("DEBUG: IP=%s, Tokens=%.2f, RemoteAddr=%s", ip, limiter.Tokens(), r.RemoteAddr)
			// }

			if !rateLimiter.Allow(ip) {
				//log.Printf("Rate limit exceeded for IP: %s", ip)
				http.Error(w, "Rate limit exceeded. Try again later.", http.StatusTooManyRequests)
				return
			}

			//metrics timing start
			start := time.Now()
			metrics.ActiveConnections.Inc()
			defer metrics.ActiveConnections.Dec()

			//log only in debug mode
			if debug {
				//capture details of incoming request
				log.Printf("Method: %s, Host: %s, URL: %s\n", r.Method, r.Host, r.URL.String())
				for key, values := range r.Header {
					log.Printf("Request Header: %s: %v", key, values)
				}
			} else {
				log.Printf("[%s] %s", r.Method, r.Host)
			}

			host := r.Host
			if host == "" {
				host = r.URL.Host
			}

			//remove port if present
			if colonIdx := strings.Index(host, ":"); colonIdx != -1 {
				host = host[:colonIdx]
			}

			// Check blocklist BEFORE handling request (works for both HTTP and HTTPS)
			if bm.IsBlocked(host) {
				metrics.BlockedRequests.Inc()

				if r.Method == http.MethodConnect {
					http.Error(w, "Forbidden", http.StatusForbidden)

				} else {
					w.Header().Set("Content-Type", "text/html")
					w.WriteHeader(http.StatusForbidden)
					w.Write([]byte(blocklist.GetBlockedResponse()))
				}
				return
			}

			//var recorder statusRecorder
			var statusCode int = 200 // default to 200
			if r.Method == http.MethodConnect {
				tunnel.HandleTunneling(w, r)
			} else {
				recorder := &statusRecorder{ResponseWriter: w, statusCode: http.StatusOK}
				handlers.HandleHTTP(recorder, r)
				statusCode = recorder.statusCode
			}

			duration := time.Since(start).Seconds()
			metrics.RequestDuration.WithLabelValues(r.Method).Observe(duration)
			statusClass := fmt.Sprintf("%dxx", statusCode/100)
			metrics.StatusCodeCounter.WithLabelValues(statusClass).Inc()
			metrics.RequestsTotal.WithLabelValues(r.Method, http.StatusText(statusCode)).Inc()
		}),
		// Disable HTTP/2 to avoid issues with CONNECT method
		TLSNextProto: make(map[string]func(*http.Server, *tls.Conn, http.Handler)),
	}

	// Start server
	log.Printf("Starting proxy server on %s (protocol: %s)", server.Addr, proto)
	if proto == "http" {
		log.Fatal(server.ListenAndServe())
	} else {
		log.Fatal(server.ListenAndServeTLS(pemPath, keyPath))
	}

}
