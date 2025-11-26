package main

import (
	"crypto/tls"
	"flag"
	"log"
	"net/http"
	"strings"

	"github.com/aluko123/go-network-proxy/inference/queue"
	"github.com/aluko123/go-network-proxy/inference/router"
	"github.com/aluko123/go-network-proxy/pkg/blocklist"
	"github.com/aluko123/go-network-proxy/pkg/limit"
	"github.com/aluko123/go-network-proxy/pkg/middleware"
	"github.com/aluko123/go-network-proxy/proxy/handlers"
	"github.com/aluko123/go-network-proxy/proxy/tunnel"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"golang.org/x/time/rate"
)

func main() {
	// --- 1. Configuration Flags ---
	var (
		pemPath      string
		keyPath      string
		proto        string
		debug        bool
		limiterType  string
		redisAddr    string
		rateLimit    int
		rateBurst    int
		workerAddrs  string
	)

	flag.StringVar(&pemPath, "pem", "server.pem", "path to pem file")
	flag.StringVar(&keyPath, "key", "server.key", "path to key file")
	flag.StringVar(&proto, "proto", "http", "protocol to use: http or https")
	flag.BoolVar(&debug, "debug", false, "enable debug logging")

	flag.StringVar(&limiterType, "limiter", "redis", "Rate limiter type: memory or redis")
	flag.StringVar(&redisAddr, "redis-addr", "localhost:6379", "Redis server address")
	flag.IntVar(&rateLimit, "rate-limit", 100, "Requests per minute per IP")
	flag.IntVar(&rateBurst, "rate-burst", 20, "Burst size for rate limiter")

	flag.StringVar(&workerAddrs, "worker-addrs", "", "Comma-separated list of inference worker addresses")

	flag.Parse()

	// --- 2. Initialize Infrastructure ---

	// Blocklist
	bm := blocklist.NewManager()
	// Note: Adjusted path to config/blocklist.json
	if err := bm.LoadFromFile("configs/blocklist.json"); err != nil {
		log.Printf("Warning: Could not load blocklist: %v", err)
	}

	// Rate Limiter
	var rateLimiter limit.RateLimiter
	var err error

	switch limiterType {
	case "redis":
		log.Printf("Initializing Redis rate limiter (addr: %s, limit: %d/min, burst: %d)", redisAddr, rateLimit, rateBurst)
		rateLimiter, err = limit.NewRedisRateLimiter(redisAddr, rateLimit, rateBurst)
		if err != nil {
			log.Fatalf("Failed to initialize Redis rate limiter: %v", err)
		}
		log.Println("✓ Redis rate limiter initialized")
	case "memory":
		log.Printf("Initializing in-memory rate limiter (limit: %d/min)", rateLimit)
		rateLimiter = limit.NewMemoryRateLimiter(rate.Limit(float64(rateLimit)/60), rateBurst)
		log.Println("✓ In-memory rate limiter initialized")
	default:
		log.Fatalf("Invalid limiter type: %s", limiterType)
	}
	defer rateLimiter.Close()

	// --- 3. Inference Engine Initialization ---
	var inferenceHandler *handlers.InferenceHandler
	
	if workerAddrs != "" {
		// 1. Create Priority Queue
		pq := queue.NewPriorityQueue()
		
		// 2. Create and Start Router (Manages Workers)
		addrs := strings.Split(workerAddrs, ",")
		routerInstance, err := router.NewRouter(addrs, pq)
		if err != nil {
			log.Fatalf("Failed to initialize inference router: %v", err)
		}
		routerInstance.Start()
		defer routerInstance.Close()
		
		// 3. Create HTTP Handler
		inferenceHandler = handlers.NewInferenceHandler(pq)
		log.Printf("✓ Inference Gateway initialized with %d workers", len(addrs))
	}

	// --- 4. Setup Handlers & Routing ---

	mux := http.NewServeMux()

	// A. Observability
	mux.Handle("/metrics", promhttp.Handler())

	// B. Inference Endpoint
	if inferenceHandler != nil {
		mux.Handle("/v1/inference", inferenceHandler)
	}

	// C. Forward Proxy (Catch-all)
	proxyHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodConnect {
			tunnel.HandleTunneling(w, r)
		} else {
			handlers.HandleHTTP(w, r)
		}
	})

	// Wrap Proxy with Blocklist
	blockedProxy := middleware.WithBlocklist(bm)(proxyHandler)
	
	mux.Handle("/", blockedProxy)

	// --- 4. Apply Global Middleware ---
	finalHandler := middleware.Chain(
		mux,
		middleware.WithLogging(debug),
		middleware.WithRateLimit(rateLimiter),
	)

	server := &http.Server{
		Addr:         ":8080",
		Handler:      finalHandler,
		TLSNextProto: make(map[string]func(*http.Server, *tls.Conn, http.Handler)),
	}

	// --- 5. Start Server ---
	log.Printf("Starting proxy server on %s (proto: %s)", server.Addr, proto)
	if proto == "http" {
		log.Fatal(server.ListenAndServe())
	} else {
		log.Fatal(server.ListenAndServeTLS(pemPath, keyPath))
	}
}
