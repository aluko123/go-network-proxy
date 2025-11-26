package limit

type RateLimiter interface {
	Allow(ip string) bool
	Close() error
}