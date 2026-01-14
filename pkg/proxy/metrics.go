package proxy

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	upstreamLatency = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "relay_upstream_latency_seconds",
		Help:    "Time spent proxying requests to upstream targets",
		Buckets: prometheus.DefBuckets,
	})
)
