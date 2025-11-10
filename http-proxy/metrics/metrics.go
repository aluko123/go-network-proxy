package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)


var (
	// Counter: Total requests
	RequestsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "proxy_requests_total",
			Help: "Total number of proxy requests",
		},
		[]string{"method", "status"},
	)

	//Counter: Blocked requests
	BlockedRequests = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "proxy_blocked_requests_total",
			Help: "Total blocked requests",
		},
	)

	// Histogram: Request duration
	RequestDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name: "proxy_request_duration_seconds",
			Help: "Request duration in seconds",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"method"},
	)

	// Gauge: Active connections
	ActiveConnections = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "proxy_active_connections",
			Help: "Number of active proxy connections",
		},
	)
)