package middleware

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	cacheHits = promauto.NewCounter(prometheus.CounterOpts{
		Name: "relay_cache_hits_total",
		Help: "Number of cache hits served from Redis",
	})
	cacheMisses = promauto.NewCounter(prometheus.CounterOpts{
		Name: "relay_cache_misses_total",
		Help: "Number of cache misses that required upstream fetch",
	})
	requestTokenHistogram = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "relay_request_tokens",
		Help:    "Token count per request payload",
		Buckets: []float64{1, 10, 50, 100, 500, 1_000, 2_000, 4_000, 8_000, 16_000},
	})
)
