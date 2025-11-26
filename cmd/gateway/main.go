package main

import (
	"context"
	"crypto/tls"
	"flag"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/aluko123/go-network-proxy/inference/queue"
	"github.com/aluko123/go-network-proxy/inference/router"
	"github.com/aluko123/go-network-proxy/inference/worker"
	"github.com/aluko123/go-network-proxy/pkg/blocklist"
	"github.com/aluko123/go-network-proxy/pkg/limit"
	"github.com/aluko123/go-network-proxy/pkg/logger"
	"github.com/aluko123/go-network-proxy/pkg/middleware"
	"github.com/aluko123/go-network-proxy/proxy/handlers"
	"github.com/aluko123/go-network-proxy/proxy/tunnel"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"golang.org/x/time/rate"
)

func main() {
	// --- 1. Configuration Flags ---
	var (
		pemPath     string
		keyPath     string
		proto       string
		debug       bool
		limiterType string
		redisAddr   string
		rateLimit   int
		rateBurst   int
		workerAddrs string
		logFormat   string

		// Timeout configuration
		readTimeout      time.Duration
		writeTimeout     time.Duration
		idleTimeout      time.Duration
		dialTimeout      time.Duration
		inferenceTimeout time.Duration
		shutdownTimeout  time.Duration
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

	flag.StringVar(&logFormat, "log-format", "json", "Log format: json or text")

	// Timeout flags
	flag.DurationVar(&readTimeout, "read-timeout", 30*time.Second, "HTTP read timeout")
	flag.DurationVar(&writeTimeout, "write-timeout", 60*time.Second, "HTTP write timeout")
	flag.DurationVar(&idleTimeout, "idle-timeout", 120*time.Second, "HTTP idle timeout")
	flag.DurationVar(&dialTimeout, "dial-timeout", 10*time.Second, "Upstream connection dial timeout")
	flag.DurationVar(&inferenceTimeout, "inference-timeout", 5*time.Minute, "Max inference request duration")
	flag.DurationVar(&shutdownTimeout, "shutdown-timeout", 30*time.Second, "Graceful shutdown timeout")

	flag.Parse()

	// --- 2. Initialize Infrastructure ---

	log := logger.New(logFormat)

	// Configure timeouts for handlers
	tunnel.SetConfig(tunnel.Config{
		DialTimeout: dialTimeout,
	})
	handlers.SetConfig(handlers.Config{
		DialTimeout:     dialTimeout,
		IdleConnTimeout: idleTimeout,
	})
	worker.SetConfig(worker.Config{
		InferenceTimeout: inferenceTimeout,
	})

	// Blocklist
	bm := blocklist.NewManager()
	// Note: Adjusted path to config/blocklist.json
	if err := bm.LoadFromFile("configs/blocklist.json"); err != nil {
		log.Warn("could not load blocklist", "error", err)
	}

	// Rate Limiter
	var rateLimiter limit.RateLimiter
	var err error

	switch limiterType {
	case "redis":
		log.Info("initializing redis rate limiter", "addr", redisAddr, "limit", rateLimit, "burst", rateBurst)
		rateLimiter, err = limit.NewRedisRateLimiter(redisAddr, rateLimit, rateBurst)
		if err != nil {
			log.Error("failed to initialize redis rate limiter", "error", err)
			os.Exit(1)
		}
		log.Info("redis rate limiter initialized")
	case "memory":
		log.Info("initializing in-memory rate limiter", "limit", rateLimit)
		rateLimiter = limit.NewMemoryRateLimiter(rate.Limit(float64(rateLimit)/60), rateBurst)
		log.Info("in-memory rate limiter initialized")
	default:
		log.Error("invalid limiter type", "type", limiterType)
		os.Exit(1)
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
			log.Error("failed to initialize inference router", "error", err)
			os.Exit(1)
		}
		routerInstance.Start()
		defer routerInstance.Close()

		// 3. Create HTTP Handler
		inferenceHandler = handlers.NewInferenceHandler(pq)
		log.Info("inference gateway initialized", "workers", len(addrs))
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
	// Chain applies in reverse order: last listed runs first
	finalHandler := middleware.Chain(
		mux,
		middleware.WithRateLimit(rateLimiter), // 3. Check rate limit
		middleware.WithLogging(log),           // 2. Log request (needs request_id)
		middleware.WithRequestID(),            // 1. Generate request ID first
	)

	server := &http.Server{
		Addr:         ":8080",
		Handler:      finalHandler,
		ReadTimeout:  readTimeout,
		WriteTimeout: writeTimeout,
		IdleTimeout:  idleTimeout,
		TLSNextProto: make(map[string]func(*http.Server, *tls.Conn, http.Handler)),
	}

	// --- 5. Start Server ---
	log.Info("starting server",
		"addr", server.Addr,
		"proto", proto,
		"read_timeout", readTimeout,
		"write_timeout", writeTimeout,
		"idle_timeout", idleTimeout,
		"shutdown_timeout", shutdownTimeout,
	)

	// Channel to receive server errors
	serverErr := make(chan error, 1)

	go func() {
		if proto == "http" {
			serverErr <- server.ListenAndServe()
		} else {
			serverErr <- server.ListenAndServeTLS(pemPath, keyPath)
		}
	}()

	// --- 6. Graceful Shutdown ---
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	select {
	case err := <-serverErr:
		if err != nil && err != http.ErrServerClosed {
			log.Error("server error", "error", err)
			os.Exit(1)
		}
	case sig := <-quit:
		log.Info("shutdown signal received", "signal", sig.String())
	}

	// Create shutdown context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()

	log.Info("shutting down server", "timeout", shutdownTimeout)

	// Shutdown HTTP server (stops accepting new connections, waits for existing)
	if err := server.Shutdown(ctx); err != nil {
		log.Error("server shutdown error", "error", err)
	}

	log.Info("server stopped gracefully")
}
