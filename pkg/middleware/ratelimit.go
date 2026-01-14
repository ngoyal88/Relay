package middleware

import (
	"context"
	"log"
	"math"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-redis/redis_rate/v10"
	"github.com/ngoyal88/relay/pkg/cache"
	"github.com/ngoyal88/relay/pkg/config"
	"golang.org/x/time/rate"
)

// NewRateLimiter enforces request limits and reads values from a hot-reloadable config store.
// If Redis is available we enforce limits globally across instances using redis_rate.
// If Redis is nil we fall back to an in-memory limiter that is recreated if RPS/Burst change.
func NewRateLimiter(rdb *cache.Client, cfgStore *config.Store) func(http.Handler) http.Handler {
	if cfgStore == nil {
		return func(next http.Handler) http.Handler { return next }
	}

	// In-memory (per-instance) limiter with dynamic updates
	if rdb == nil {
		var (
			mu      sync.Mutex
			limiter *rate.Limiter
			lastRPS float64
			lastB   int
		)

		return func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				cfg := cfgStore.Get()
				if cfg == nil || !cfg.RateLimit.Enabled {
					next.ServeHTTP(w, r)
					return
				}

				if cfg.RateLimit.RPS <= 0 {
					http.Error(w, "Too Many Requests", http.StatusTooManyRequests)
					return
				}

				burst := cfg.RateLimit.Burst
				if burst < 1 {
					burst = 1
				}

				mu.Lock()
				if limiter == nil || cfg.RateLimit.RPS != lastRPS || burst != lastB {
					limiter = rate.NewLimiter(rate.Limit(cfg.RateLimit.RPS), burst)
					lastRPS = cfg.RateLimit.RPS
					lastB = burst
				}
				l := limiter
				mu.Unlock()

				if !l.Allow() {
					http.Error(w, "Too Many Requests", http.StatusTooManyRequests)
					return
				}
				next.ServeHTTP(w, r)
			})
		}
	}

	// Distributed limiter backed by Redis; limits are recomputed on each request from live config.
	redisLimiter := redis_rate.NewLimiter(rdb.Redis())

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			cfg := cfgStore.Get()
			if cfg == nil || !cfg.RateLimit.Enabled {
				next.ServeHTTP(w, r)
				return
			}

			limit, ok := buildLimit(cfg.RateLimit.RPS, cfg.RateLimit.Burst)
			if !ok {
				http.Error(w, "Too Many Requests", http.StatusTooManyRequests)
				return
			}

			key := clientKey(r)
			ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
			defer cancel()

			res, err := redisLimiter.Allow(ctx, key, limit)
			if err != nil {
				log.Printf("[RATE] redis error: %v (allowing request)", err)
				next.ServeHTTP(w, r)
				return
			}

			if res.Allowed == 0 {
				if res.RetryAfter > 0 {
					retrySeconds := res.RetryAfter / time.Second
					if res.RetryAfter%time.Second != 0 {
						retrySeconds++
					}
					w.Header().Set("Retry-After", strconv.FormatInt(int64(retrySeconds), 10))
				}
				http.Error(w, "Too Many Requests", http.StatusTooManyRequests)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

func buildLimit(rps float64, burst int) (redis_rate.Limit, bool) {
	if rps <= 0 {
		return redis_rate.Limit{}, false
	}
	if burst < 1 {
		burst = 1
	}

	period := time.Second
	ratePerPeriod := int(math.Ceil(rps))
	if rps > 0 && rps < 1 {
		periodSeconds := math.Ceil(1.0 / rps)
		period = time.Duration(periodSeconds) * time.Second
		ratePerPeriod = 1
	}

	return redis_rate.Limit{Rate: ratePerPeriod, Burst: burst, Period: period}, true
}

// clientKey produces a stable key per client using forwarded IP if present.
func clientKey(r *http.Request) string {
	if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
		parts := strings.Split(fwd, ",")
		return strings.TrimSpace(parts[0])
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		return host
	}
	return r.RemoteAddr
}
