package main

import (
	"context"
	"crypto/tls"
	"flag"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/aluko123/go-network-proxy/inference/backend"
	"github.com/aluko123/go-network-proxy/inference/queue"
	"github.com/aluko123/go-network-proxy/inference/router"
	"github.com/aluko123/go-network-proxy/pkg/auth"
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
	var (
		pemPath      string
		keyPath      string
		proto        string
		limiterType  string
		redisAddr    string
		rateLimit    int
		rateBurst    int
		backendsFile string
		logFormat    string
		routerWorkers int

		readTimeout      time.Duration
		writeTimeout     time.Duration
		idleTimeout      time.Duration
		dialTimeout      time.Duration
		shutdownTimeout  time.Duration
	)

	flag.StringVar(&pemPath, "pem", "server.pem", "path to pem file")
	flag.StringVar(&keyPath, "key", "server.key", "path to key file")
	flag.StringVar(&proto, "proto", "http", "protocol to use: http or https")

	flag.StringVar(&limiterType, "limiter", "redis", "Rate limiter type: memory or redis")
	flag.StringVar(&redisAddr, "redis-addr", "localhost:6379", "Redis server address")
	flag.IntVar(&rateLimit, "rate-limit", 100, "Requests per minute per IP")
	flag.IntVar(&rateBurst, "rate-burst", 20, "Burst size for rate limiter")

	flag.StringVar(&backendsFile, "backends", "configs/backends.yaml", "Path to backends config file")
	flag.IntVar(&routerWorkers, "router-workers", 10, "Number of router worker goroutines")

	flag.StringVar(&logFormat, "log-format", "json", "Log format: json or text")

	flag.DurationVar(&readTimeout, "read-timeout", 30*time.Second, "HTTP read timeout")
	flag.DurationVar(&writeTimeout, "write-timeout", 60*time.Second, "HTTP write timeout")
	flag.DurationVar(&idleTimeout, "idle-timeout", 120*time.Second, "HTTP idle timeout")
	flag.DurationVar(&dialTimeout, "dial-timeout", 10*time.Second, "Upstream connection dial timeout")
	flag.DurationVar(&shutdownTimeout, "shutdown-timeout", 30*time.Second, "Graceful shutdown timeout")

	flag.Parse()

	log := logger.New(logFormat)

	tunnel.SetConfig(tunnel.Config{
		DialTimeout: dialTimeout,
	})
	handlers.SetConfig(handlers.Config{
		DialTimeout:     dialTimeout,
		IdleConnTimeout: idleTimeout,
	})

	bm := blocklist.NewManager()
	if err := bm.LoadFromFile("configs/blocklist.json"); err != nil {
		log.Warn("could not load blocklist", "error", err)
	}

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

	// --- Inference Engine with Backend Registry ---
	var inferenceHandler *handlers.InferenceHandler
	var routerInstance *router.Router

	registry, err := backend.LoadRegistry(backendsFile)
	if err != nil {
		log.Warn("could not load backends config", "error", err, "file", backendsFile)
		registry = backend.NewRegistry()
	} else {
		models := registry.ListModels()
		backends := registry.ListBackends()
		log.Info("loaded backends",
			"backends", len(backends),
			"models", len(models),
		)
		for _, b := range backends {
			log.Info("backend registered",
				"name", b.Name(),
				"type", b.Type(),
				"models", b.Models(),
			)
		}
	}

	pq := queue.NewPriorityQueue()
	routerInstance = router.NewRouter(registry, pq, routerWorkers)
	routerInstance.Start()
	defer routerInstance.Close()

	inferenceHandler = handlers.NewInferenceHandler(pq)
	log.Info("inference gateway initialized", "router_workers", routerWorkers)

	// --- Setup Handlers & Routing ---
	mux := http.NewServeMux()

	mux.Handle("/metrics", promhttp.Handler())

	// Models endpoint - list available models
	mux.HandleFunc("/v1/models", func(w http.ResponseWriter, r *http.Request) {
		models := registry.ListModels()
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"models":[`))
		for i, m := range models {
			if i > 0 {
				w.Write([]byte(","))
			}
			w.Write([]byte(`"` + m + `"`))
		}
		w.Write([]byte(`]}`))
	})

	// Inference endpoint with auth
	keyStore := auth.NewKeyStore()
	if err := keyStore.LoadFromFile("configs/apikeys.json"); err != nil {
		log.Warn("could not load API keys", "error", err)
	} else {
		log.Info("loaded API keys", "count", keyStore.Count())
	}

	authedInference := middleware.WithAPIKeyAuth(keyStore)(inferenceHandler)
	mux.Handle("/v1/inference", authedInference)

	// Forward Proxy (Catch-all)
	proxyHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodConnect {
			tunnel.HandleTunneling(w, r)
		} else {
			handlers.HandleHTTP(w, r)
		}
	})

	blockedProxy := middleware.WithBlocklist(bm)(proxyHandler)
	mux.Handle("/", blockedProxy)

	finalHandler := middleware.Chain(
		mux,
		middleware.WithRateLimit(rateLimiter),
		middleware.WithLogging(log),
		middleware.WithRequestID(),
	)

	server := &http.Server{
		Addr:         ":8080",
		Handler:      finalHandler,
		ReadTimeout:  readTimeout,
		WriteTimeout: writeTimeout,
		IdleTimeout:  idleTimeout,
		TLSNextProto: make(map[string]func(*http.Server, *tls.Conn, http.Handler)),
	}

	log.Info("starting server",
		"addr", server.Addr,
		"proto", proto,
		"read_timeout", readTimeout,
		"write_timeout", writeTimeout,
		"idle_timeout", idleTimeout,
		"shutdown_timeout", shutdownTimeout,
	)

	serverErr := make(chan error, 1)

	go func() {
		if proto == "http" {
			serverErr <- server.ListenAndServe()
		} else {
			serverErr <- server.ListenAndServeTLS(pemPath, keyPath)
		}
	}()

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

	ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()

	log.Info("shutting down server", "timeout", shutdownTimeout)

	if err := server.Shutdown(ctx); err != nil {
		log.Error("server shutdown error", "error", err)
	}

	log.Info("server stopped gracefully")
}
