package main

import (
	"fmt"
	"log"
	"net/http"

	"github.com/ngoyal88/relay/pkg/cache" // Check this path matches your go.mod
	"github.com/ngoyal88/relay/pkg/config"
	"github.com/ngoyal88/relay/pkg/middleware"
	"github.com/ngoyal88/relay/pkg/proxy"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

func main() {
	// 1. Load Config with hot reload
	cfgStore, err := config.LoadAndWatch()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}
	cfg := cfgStore.Get()
	if cfg == nil {
		log.Fatal("Config could not be read")
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

	// Layer A: Rate Limiter (distributed if Redis is available; in-memory fallback) with live config
	handler = middleware.NewRateLimiter(rdb, cfgStore)(handler)

	// Layer B: Caching (Only if Redis is connected)
	if cfg.Redis.Enabled && rdb != nil {
		handler = middleware.CachingMiddleware(rdb)(handler)
	}

	// Layer C: Cost Tracking (uses live pricing from config store)
	handler = middleware.TokenCostLogger(cfgStore)(handler)

	// Layer D: Request Logging (Outer-most)
	handler = middleware.RequestLogger(handler)

	// 5. Expose metrics and start server
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())
	mux.Handle("/", handler)

	fmt.Printf("Relay starting on %s\n", cfg.Server.Port)
	if err := http.ListenAndServe(cfg.Server.Port, mux); err != nil {
		log.Fatal("Server failed:", err)
	}
}
