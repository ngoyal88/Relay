package main

import (
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/ngoyal88/relay/pkg/api"
	"github.com/ngoyal88/relay/pkg/cache"
	"github.com/ngoyal88/relay/pkg/config"
	"github.com/ngoyal88/relay/pkg/keymanager"
	"github.com/ngoyal88/relay/pkg/middleware"
	"github.com/ngoyal88/relay/pkg/proxy"
	"github.com/ngoyal88/relay/pkg/storage"
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

	// 2. Initialize Redis (if enabled)
	var rdb *cache.Client
	if cfg.Redis.Enabled {
		rdb, err = cache.NewRedis(cfg.Redis.Address, cfg.Redis.Password, cfg.Redis.DB)
		if err != nil {
			log.Fatalf("Could not connect to Redis: %v", err)
		}
		fmt.Println("✅ Connected to Redis successfully!")
	}

	// 3. Initialize Storage (for request logging)
	var store storage.Store
	if cfg.Logging.Enabled && rdb != nil {
		retentionDays := cfg.Logging.RetentionDays
		if retentionDays == 0 {
			retentionDays = 30
		}
		store = storage.NewRedisStore(rdb, time.Duration(retentionDays)*24*time.Hour)
		fmt.Println("✅ Request logging enabled")
	}

	var km *keymanager.Manager
	if rdb != nil {
		km = keymanager.New(rdb)
	}

	// 4. Create Proxy or Load Balancer
	var handler http.Handler

	if cfg.LoadBalancer.Enabled && len(cfg.LoadBalancer.Targets) > 0 {
		targets := make([]proxy.TargetConfig, 0, len(cfg.LoadBalancer.Targets))
		for _, t := range cfg.LoadBalancer.Targets {
			targets = append(targets, proxy.TargetConfig{URL: t.URL, Weight: t.Weight})
		}
		// Use load balancer with multiple targets
		lb, err := proxy.NewLoadBalancer(targets, cfg.LoadBalancer.Strategy)
		if err != nil {
			log.Fatalf("Failed to create load balancer: %v", err)
		}
		handler = lb
		fmt.Printf("✅ Load balancer started with %d targets (strategy: %s)\n",
			len(cfg.LoadBalancer.Targets), cfg.LoadBalancer.Strategy)
	} else {
		// Use single target proxy
		gw, err := proxy.New(cfg.Proxy.Target)
		if err != nil {
			log.Fatal("Failed to create relay:", err)
		}
		handler = gw
		fmt.Printf("✅ Proxy started targeting: %s\n", cfg.Proxy.Target)
	}

	// 5. Chain Middleware (order matters!)
	// Start with the inner-most handler (The Proxy/Load Balancer)

	// Layer A: Request Transformation (if enabled)
	if cfg.Transform.Enabled {
		transformCfg := middleware.TransformConfig{
			RemoveHeaders:     cfg.Transform.RemoveHeaders,
			AddHeaders:        cfg.Transform.AddHeaders,
			ReplaceHeaders:    cfg.Transform.ReplaceHeaders,
			RequestRules:      toTransformRules(cfg.Transform.RequestRules),
			ResponseRules:     toTransformRules(cfg.Transform.ResponseRules),
			MaskSensitiveData: cfg.Transform.MaskSensitiveData,
			AllowedPaths:      cfg.Transform.AllowedPaths,
			BlockedPaths:      cfg.Transform.BlockedPaths,
		}
		handler = middleware.TransformMiddleware(transformCfg)(handler)
		fmt.Println("✅ Request transformation enabled")
	}

	// Layer B: Rate Limiter (distributed if Redis is available)
	handler = middleware.NewRateLimiter(rdb, cfgStore)(handler)
	if cfg.RateLimit.Enabled {
		fmt.Printf("✅ Rate limiting: %.1f req/s (burst: %d)\n",
			cfg.RateLimit.RPS, cfg.RateLimit.Burst)
	}

	// Layer C: Caching (Only if Redis is connected)
	if cfg.Redis.Enabled && rdb != nil {
		handler = middleware.CachingMiddleware(rdb)(handler)
		fmt.Println("✅ Response caching enabled")
	}

	// Layer D: Authentication (if enabled)
	if cfg.Auth.Enabled {
		if rdb == nil {
			log.Fatal("Authentication requires Redis to be enabled")
		}
		handler = middleware.AuthMiddleware(rdb, true)(handler)
		fmt.Println("✅ API key authentication enabled")
	}

	// Layer E: Request/Response Logging (if enabled)
	if cfg.Logging.Enabled && store != nil {
		handler = middleware.RequestLoggingMiddleware(store, true)(handler)
		fmt.Printf("✅ Request logging enabled (retention: %d days)\n", cfg.Logging.RetentionDays)
	}

	// Layer F: Cost Tracking (uses live pricing from config store)
	handler = middleware.TokenCostLogger(cfgStore)(handler)

	// Layer G: Request Logger (Outer-most - console logging)
	handler = middleware.RequestLogger(handler)

	// 6. Setup HTTP Server
	mux := http.NewServeMux()

	// Metrics endpoint
	mux.Handle("/metrics", promhttp.Handler())

	// Health check endpoint
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	// Admin API
	if km != nil && cfg.Auth.AdminKey != "" {
		adminAPI := api.NewAdminAPI(km, store, cfg.Auth.AdminKey)
		adminAPI.RegisterRoutes(mux)
		fmt.Println("✅ Admin API enabled at /admin/*")
	} else if cfg.Auth.AdminKey != "" && km == nil {
		log.Println("⚠️  Admin API not enabled: Redis is required")
	}

	// Main handler
	mux.Handle("/", handler)

	// 7. Start Server
	fmt.Println("\n🚀 Relay Features Active:")
	fmt.Println("   - Metrics:         http://localhost" + cfg.Server.Port + "/metrics")
	fmt.Println("   - Health Check:    http://localhost" + cfg.Server.Port + "/health")
	fmt.Println("   - Main Endpoint:   http://localhost" + cfg.Server.Port)
	fmt.Println("\n📊 Configuration can be hot-reloaded by editing configs/config.yaml")
	fmt.Printf("\n🎯 Server listening on %s\n", cfg.Server.Port)

	if err := http.ListenAndServe(cfg.Server.Port, mux); err != nil {
		log.Fatal("Server failed:", err)
	}
}

func toTransformRules(in []config.TransformRule) []middleware.TransformRule {
	if len(in) == 0 {
		return nil
	}

	out := make([]middleware.TransformRule, 0, len(in))
	for _, rule := range in {
		out = append(out, middleware.TransformRule{
			Type:    rule.Type,
			Path:    rule.Path,
			Value:   rule.Value,
			Pattern: rule.Pattern,
			Replace: rule.Replace,
		})
	}
	return out
}
