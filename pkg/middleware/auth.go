package middleware

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/ngoyal88/relay/pkg/cache"
	"github.com/redis/go-redis/v9"
)

// APIKey represents an API key with metadata
type APIKey struct {
	Key         string     `json:"key"`
	Name        string     `json:"name"`
	UserID      string     `json:"user_id"`
	RateLimit   float64    `json:"rate_limit"` // requests per second
	Burst       int        `json:"burst"`
	Quota       int64      `json:"quota"` // total requests allowed
	Used        int64      `json:"used"`  // requests used
	Active      bool       `json:"active"`
	CreatedAt   time.Time  `json:"created_at"`
	ExpiresAt   *time.Time `json:"expires_at,omitempty"`
	LastUsedAt  *time.Time `json:"last_used_at,omitempty"`
	Description string     `json:"description,omitempty"`
}

type contextKey string

const apiKeyContextKey contextKey = "api_key"
const tokenCountContextKey contextKey = "token_count"
const tokenCostContextKey contextKey = "token_cost"

// AuthMiddleware validates API keys and enforces per-key limits
func AuthMiddleware(rdb *cache.Client, enableAuth bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Skip auth if disabled
			if !enableAuth {
				next.ServeHTTP(w, r)
				return
			}

			// Extract API key from Authorization header
			// Format: "Bearer relay_xxxxxxxxxxxxx"
			authHeader := r.Header.Get("Authorization")
			if authHeader == "" {
				respondError(w, "Missing Authorization header", http.StatusUnauthorized)
				return
			}

			parts := strings.SplitN(authHeader, " ", 2)
			if len(parts) != 2 || strings.ToLower(parts[0]) != "bearer" {
				respondError(w, "Invalid Authorization format. Use: Bearer <api_key>", http.StatusUnauthorized)
				return
			}

			apiKeyStr := parts[1]
			if !strings.HasPrefix(apiKeyStr, "relay_") {
				respondError(w, "Invalid API key format", http.StatusUnauthorized)
				return
			}

			// Validate and load API key
			ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
			defer cancel()

			apiKey, err := validateAPIKey(ctx, rdb, apiKeyStr)
			if err != nil {
				respondError(w, fmt.Sprintf("Invalid API key: %v", err), http.StatusUnauthorized)
				return
			}

			// Check if key is active
			if !apiKey.Active {
				respondError(w, "API key is inactive", http.StatusForbidden)
				return
			}

			// Check expiration
			if apiKey.ExpiresAt != nil && time.Now().After(*apiKey.ExpiresAt) {
				respondError(w, "API key has expired", http.StatusForbidden)
				return
			}

			// Check quota
			if apiKey.Quota > 0 && apiKey.Used >= apiKey.Quota {
				respondError(w, "API key quota exceeded", http.StatusTooManyRequests)
				return
			}

			// Update usage (async to not slow down request)
			go func(key string) {
				ctx := context.Background()
				incrementUsage(ctx, rdb, key)
			}(apiKeyStr)

			// Store API key in context for downstream middleware
			ctx = context.WithValue(r.Context(), apiKeyContextKey, apiKey)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// validateAPIKey checks if an API key exists and is valid
func validateAPIKey(ctx context.Context, rdb *cache.Client, key string) (*APIKey, error) {
	if rdb == nil {
		return nil, fmt.Errorf("redis not configured")
	}

	// Get from Redis
	keyData := fmt.Sprintf("apikey:%s", key)
	data, err := rdb.Get(ctx, keyData)
	if err != nil {
		if err == redis.Nil {
			return nil, fmt.Errorf("key not found")
		}
		return nil, err
	}

	var apiKey APIKey
	if err := json.Unmarshal(data, &apiKey); err != nil {
		return nil, fmt.Errorf("corrupted key data")
	}

	return &apiKey, nil
}

// incrementUsage updates the usage counter for an API key
func incrementUsage(ctx context.Context, rdb *cache.Client, key string) {
	if rdb == nil {
		return
	}

	keyData := fmt.Sprintf("apikey:%s", key)

	// Get current key
	data, err := rdb.Get(ctx, keyData)
	if err != nil {
		return
	}

	var apiKey APIKey
	if err := json.Unmarshal(data, &apiKey); err != nil {
		return
	}

	// Update usage and last used
	apiKey.Used++
	now := time.Now()
	apiKey.LastUsedAt = &now

	// Save back
	updated, _ := json.Marshal(apiKey)
	rdb.Set(ctx, keyData, updated, 0) // No expiration for keys
}

// GetAPIKeyFromContext retrieves the API key from request context
func GetAPIKeyFromContext(ctx context.Context) (*APIKey, bool) {
	apiKey, ok := ctx.Value(apiKeyContextKey).(*APIKey)
	return apiKey, ok
}

// GetTokenCountFromContext returns the token count set by TokenCostLogger.
func GetTokenCountFromContext(ctx context.Context) (int, bool) {
	val, ok := ctx.Value(tokenCountContextKey).(int)
	return val, ok
}

// GetTokenCostFromContext returns the estimated request cost set by TokenCostLogger.
func GetTokenCostFromContext(ctx context.Context) (float64, bool) {
	val, ok := ctx.Value(tokenCostContextKey).(float64)
	return val, ok
}

func respondError(w http.ResponseWriter, message string, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{
		"error": message,
	})
}
