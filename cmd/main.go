package main

import (
	"fmt"
	"log"
	"net/http"

		"github.com/ngoyal88/relay/pkg/cache" // Check this path matches your go.mod
		"github.com/ngoyal88/relay/pkg/config"
		"github.com/ngoyal88/relay/pkg/middleware"
		"github.com/ngoyal88/relay/pkg/proxy"
	"golang.org/x/time/rate"
)

func main() {
	// 1. Load Config
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// 2. Initialize Redis
	// FIX: Declare rdb OUTSIDE the if block
	var rdb *cache.Client

	if cfg.Redis.Enabled {
		// FIX: Use '=' (Assignment), not ':=' (New Variable)
		rdb, err = cache.NewRedis(cfg.Redis.Address, cfg.Redis.Password, cfg.Redis.DB)
		if err != nil {
			log.Fatalf("Could not connect to Redis: %v", err)
		}
		fmt.Println("✅ Connected to Redis successfully!")
	}

	// 3. Create the Proxy
	gw, err := proxy.New(cfg.Proxy.Target)
	if err != nil {
		log.Fatal("Failed to create relay:", err)
	}

	// 4. Chain Middleware
	// Start with the inner-most handler (The Proxy)
	var handler http.Handler = gw

	// Layer A: Rate Limiter
	if cfg.RateLimit.Enabled {
		rps := rate.Limit(cfg.RateLimit.RPS)
		handler = middleware.NewRateLimiter(rps, cfg.RateLimit.Burst)(handler)
	}

	// Layer B: Caching (Only if Redis is connected)
	if cfg.Redis.Enabled && rdb != nil {
		handler = middleware.CachingMiddleware(rdb)(handler)
	}

	// Layer C: Cost Tracking
	handler = middleware.TokenCostLogger(handler)

	// Layer D: Request Logging (Outer-most)
	handler = middleware.RequestLogger(handler)

	// 5. Start Server
	fmt.Printf("Relay starting on %s\n", cfg.Server.Port)
	if err := http.ListenAndServe(cfg.Server.Port, handler); err != nil {
		log.Fatal("Server failed:", err)
	}
}
