package proxy

import (
	"context"
	"fmt"
	"math/rand"
	"net/http"
	"net/http/httputil"
	"net/url"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sony/gobreaker"
)

// Target represents a backend target with its configuration
type Target struct {
	URL            *url.URL
	Weight         int
	Proxy          *httputil.ReverseProxy
	CircuitBreaker *gobreaker.CircuitBreaker
	Healthy        atomic.Bool
	LastCheck      time.Time
	mu             sync.RWMutex
}

// LoadBalancer manages multiple targets with different strategies
type LoadBalancer struct {
	targets  []*Target
	strategy string // "round-robin", "weighted", "least-latency", "random"
	current  atomic.Uint64
	latency  map[string]*LatencyTracker
	mu       sync.RWMutex
}

// TargetConfig represents target configuration
type TargetConfig struct {
	URL    string `mapstructure:"url"`
	Weight int    `mapstructure:"weight"`
}

// LatencyTracker tracks response times for a target
type LatencyTracker struct {
	mu      sync.Mutex
	samples []time.Duration
	maxSize int
}

// NewLoadBalancer creates a new load balancer
func NewLoadBalancer(configs []TargetConfig, strategy string) (*LoadBalancer, error) {
	if len(configs) == 0 {
		return nil, fmt.Errorf("no targets configured")
	}

	lb := &LoadBalancer{
		targets:  make([]*Target, 0, len(configs)),
		strategy: strategy,
		latency:  make(map[string]*LatencyTracker),
	}

	for _, cfg := range configs {
		parsedURL, err := url.Parse(cfg.URL)
		if err != nil {
			return nil, fmt.Errorf("invalid target URL %s: %w", cfg.URL, err)
		}

		weight := cfg.Weight
		if weight <= 0 {
			weight = 1
		}

		proxy := httputil.NewSingleHostReverseProxy(parsedURL)
		proxy.Director = func(req *http.Request) {
			req.URL.Scheme = parsedURL.Scheme
			req.URL.Host = parsedURL.Host
			req.Header.Set("X-Relay", "True")
		}

		// Circuit breaker per target
		cb := gobreaker.NewCircuitBreaker(gobreaker.Settings{
			Name:    fmt.Sprintf("target-%s", parsedURL.Host),
			Timeout: 30 * time.Second,
			ReadyToTrip: func(c gobreaker.Counts) bool {
				return c.ConsecutiveFailures >= 5
			},
		})

		target := &Target{
			URL:            parsedURL,
			Weight:         weight,
			Proxy:          proxy,
			CircuitBreaker: cb,
		}
		target.Healthy.Store(true)

		lb.targets = append(lb.targets, target)
		lb.latency[parsedURL.String()] = NewLatencyTracker(100)
	}

	// Start health checks
	go lb.healthCheckLoop()

	return lb, nil
}

// ServeHTTP implements http.Handler
func (lb *LoadBalancer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	target, err := lb.selectTarget()
	if err != nil {
		http.Error(w, "No healthy backends available", http.StatusServiceUnavailable)
		return
	}

	// Track latency
	start := time.Now()
	defer func() {
		latency := time.Since(start)
		lb.recordLatency(target.URL.String(), latency)
	}()

	// Use circuit breaker
	rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}

	_, err = target.CircuitBreaker.Execute(func() (interface{}, error) {
		target.Proxy.ServeHTTP(rec, r)
		if rec.status >= 500 {
			return nil, fmt.Errorf("upstream error: %d", rec.status)
		}
		return nil, nil
	})

	if err != nil {
		if err == gobreaker.ErrOpenState || err == gobreaker.ErrTooManyRequests {
			http.Error(w, "Service Unavailable (circuit open)", http.StatusServiceUnavailable)
		}
	}
}

// selectTarget chooses a backend based on the configured strategy
func (lb *LoadBalancer) selectTarget() (*Target, error) {
	lb.mu.RLock()
	defer lb.mu.RUnlock()

	// Filter healthy targets
	healthy := make([]*Target, 0, len(lb.targets))
	for _, t := range lb.targets {
		if t.Healthy.Load() && t.CircuitBreaker.State() != gobreaker.StateOpen {
			healthy = append(healthy, t)
		}
	}

	if len(healthy) == 0 {
		return nil, fmt.Errorf("no healthy targets")
	}

	switch lb.strategy {
	case "round-robin":
		return lb.roundRobin(healthy), nil
	case "weighted":
		return lb.weighted(healthy), nil
	case "least-latency":
		return lb.leastLatency(healthy), nil
	case "random":
		return healthy[rand.Intn(len(healthy))], nil
	default:
		return lb.roundRobin(healthy), nil
	}
}

// roundRobin selects targets in a circular manner
func (lb *LoadBalancer) roundRobin(targets []*Target) *Target {
	// Subtract 1 so the first call uses index 0.
	idx := (lb.current.Add(1) - 1) % uint64(len(targets))
	return targets[idx]
}

// weighted selects based on configured weights
func (lb *LoadBalancer) weighted(targets []*Target) *Target {
	// Calculate total weight
	totalWeight := 0
	for _, t := range targets {
		totalWeight += t.Weight
	}

	// Random selection weighted by weight values
	random := rand.Intn(totalWeight)
	for _, t := range targets {
		random -= t.Weight
		if random < 0 {
			return t
		}
	}

	return targets[0]
}

// leastLatency selects target with lowest average latency
func (lb *LoadBalancer) leastLatency(targets []*Target) *Target {
	var best *Target
	var bestLatency time.Duration = time.Hour

	for _, t := range targets {
		avg := lb.getAverageLatency(t.URL.String())
		if avg < bestLatency {
			bestLatency = avg
			best = t
		}
	}

	if best == nil {
		return targets[0]
	}

	return best
}

// recordLatency stores latency measurement
func (lb *LoadBalancer) recordLatency(targetURL string, latency time.Duration) {
	if tracker, ok := lb.latency[targetURL]; ok {
		tracker.Add(latency)
	}
}

// getAverageLatency calculates average latency for a target
func (lb *LoadBalancer) getAverageLatency(targetURL string) time.Duration {
	tracker, ok := lb.latency[targetURL]
	if !ok {
		return time.Millisecond * 100 // Default
	}
	return tracker.Average()
}

// healthCheckLoop periodically checks target health
func (lb *LoadBalancer) healthCheckLoop() {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		for _, target := range lb.targets {
			go lb.checkHealth(target)
		}
	}
}

// checkHealth performs health check on a target
func (lb *LoadBalancer) checkHealth(target *Target) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Simple HTTP GET to /health or root
	healthURL := target.URL.String() + "/health"
	req, err := http.NewRequestWithContext(ctx, "GET", healthURL, nil)
	if err != nil {
		target.Healthy.Store(false)
		return
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		target.Healthy.Store(false)
		return
	}
	defer resp.Body.Close()

	// Consider 2xx and 404 (no health endpoint) as healthy
	if resp.StatusCode < 500 {
		target.Healthy.Store(true)
	} else {
		target.Healthy.Store(false)
	}

	target.mu.Lock()
	target.LastCheck = time.Now()
	target.mu.Unlock()
}

// NewLatencyTracker creates a new latency tracker
func NewLatencyTracker(maxSamples int) *LatencyTracker {
	return &LatencyTracker{
		samples: make([]time.Duration, 0, maxSamples),
		maxSize: maxSamples,
	}
}

// Add records a latency sample
func (lt *LatencyTracker) Add(d time.Duration) {
	lt.mu.Lock()
	defer lt.mu.Unlock()

	if len(lt.samples) >= lt.maxSize {
		// Remove oldest sample
		lt.samples = lt.samples[1:]
	}
	lt.samples = append(lt.samples, d)
}

// Average calculates average latency
func (lt *LatencyTracker) Average() time.Duration {
	lt.mu.Lock()
	defer lt.mu.Unlock()

	if len(lt.samples) == 0 {
		return 0
	}

	var total time.Duration
	for _, d := range lt.samples {
		total += d
	}
	return total / time.Duration(len(lt.samples))
}
