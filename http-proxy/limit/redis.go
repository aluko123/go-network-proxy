package limit

import (
	"context"
	"embed"
	"fmt"
	"log"
	"strings"
	"sync/atomic"
	"time"

	"github.com/redis/go-redis/v9"
)

//go:embed redis_script.lua
var scriptFS embed.FS

type RedisRateLimiter struct {
	client    *redis.Client
	script    *redis.Script
	scriptSHA string
	limit     int64
	window    time.Duration
	ctx       context.Context

	// Performance tracking
	evalShaHits   uint64
	evalFallbacks uint64
}

// Remove RedisConfig - use simple constructor for Phase 1

// NewRedisRateLimiter creates a Redis-based rate limiter with EVALSHA optimization
func NewRedisRateLimiter(addr string, limit int, window time.Duration) (*RedisRateLimiter, error) {
	client := redis.NewClient(&redis.Options{
		Addr:         addr,
		DB:           0,
		PoolSize:     100,        // Optimize connection pool
		MinIdleConns: 10,
	})

	ctx := context.Background()
	if err := client.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("redis connection failed: %w", err)
	}

	// Load Lua script
	scriptContent, err := scriptFS.ReadFile("redis_script.lua")
	if err != nil {
		return nil, fmt.Errorf("failed to read redis script: %w", err)
	}

	script := redis.NewScript(string(scriptContent))

	r := &RedisRateLimiter{
		client: client,
		script: script,
		limit:  int64(limit),
		window: window,
		ctx:    ctx,
	}

	// Preload script and cache SHA (optimization)
	if err := r.preloadScript(); err != nil {
		log.Printf("Warning: Could not preload script: %v", err)
		// Continue anyway - will fallback to EVAL
	}

	return r, nil
}

func (r *RedisRateLimiter) preloadScript() error {
	sha, err := r.script.Load(r.ctx, r.client).Result()
	if err != nil {
		return fmt.Errorf("failed to load script: %w", err)
	}
	r.scriptSHA = sha
	log.Printf("Redis rate limiter script loaded with SHA: %s", sha)
	return nil
}

func (r *RedisRateLimiter) Allow(ip string) bool {
	key := "proxy:ratelimit:" + ip
	currentTime := time.Now().UnixMilli()
	windowMs := r.window.Milliseconds()
	args := []any{r.limit, windowMs, currentTime}

	// Try EVALSHA first (optimized path)
	if r.scriptSHA != "" {
		result, err := r.evalSHA(key, args)
		if err == nil {
			atomic.AddUint64(&r.evalShaHits, 1)
			return result == 1
		}

		// NOSCRIPT error? Reload and retry once
		if isNoScriptErr(err) {
			log.Printf("Script not cached, reloading...")
			r.preloadScript()

			result, err := r.evalSHA(key, args)
			if err == nil {
				return result == 1
			}
		}

		// EVALSHA failed, fallback to EVAL
		atomic.AddUint64(&r.evalFallbacks, 1)
	}

	// Fallback: Use EVAL (sends full script)
	result, err := r.eval(key, args)
	if err != nil {
		log.Printf("Redis error: %v", err)
		return true // Fail open
	}

	return result == 1
}

func (r *RedisRateLimiter) evalSHA(key string, args []any) (int64, error) {
	return r.client.EvalSha(
		r.ctx,
		r.scriptSHA,
		[]string{key},
		args...,
	).Int64()
}

func (r *RedisRateLimiter) eval(key string, args []any) (int64, error) {
	return r.script.Run(
		r.ctx,
		r.client,
		[]string{key},
		args...,
	).Int64()
}

func isNoScriptErr(err error) bool {
	return err != nil && strings.Contains(err.Error(), "NOSCRIPT")
}

func (r *RedisRateLimiter) Close() error {
	return r.client.Close()
}
