package middleware

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/ngoyal88/relay/pkg/cache"
)

// responseWrapper "wraps" the standard ResponseWriter.
// It acts like a Spy: it writes data to the user AND saves a copy in memory.
type responseWrapper struct {
	http.ResponseWriter // Embed the original interface
	body       bytes.Buffer // Our secret storage
	statusCode int
}

// WriteHeader captures the status code (e.g., 200 or 404)
func (rw *responseWrapper) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

// Write captures the actual data (JSON body)
func (rw *responseWrapper) Write(b []byte) (int, error) {
	// FIX: If no status code was set, assume 200 OK
	if rw.statusCode == 0 {
		rw.statusCode = http.StatusOK
	}
	
	rw.body.Write(b)                  // Copy to our buffer
	return rw.ResponseWriter.Write(b) // Send to user
}

// CachingMiddleware handles the Redis logic
func CachingMiddleware(rdb *cache.Client) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// 1. Only cache POST requests
			if r.Method != http.MethodPost {
				next.ServeHTTP(w, r)
				return
			}

			// 2. Hash the Body
			bodyBytes, _ := io.ReadAll(r.Body)
			r.Body = io.NopCloser(bytes.NewBuffer(bodyBytes)) // Refill
			hash := sha256.Sum256(bodyBytes)
			key := fmt.Sprintf("cache:%s", hex.EncodeToString(hash[:]))

			// 3. CHECK REDIS (With Timeout!)
			// FIX: Don't wait forever. Give Redis 2 seconds max.
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()

			val, err := rdb.Get(ctx, key)
			if err == nil {
				// HIT!
				w.Header().Set("X-Cache", "HIT")
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				w.Write(val)
				log.Printf("⚡ [CACHE] HIT for key %s", key[:8])
				return
			} else if err != context.DeadlineExceeded && err.Error() != "redis: nil" {
				// Log actual Redis errors (connection refused, etc)
				log.Printf("⚠️ [CACHE] Redis error: %v", err)
			}

			// 4. MISS -> Proxy
			spy := &responseWrapper{ResponseWriter: w}
			next.ServeHTTP(spy, r)

			// 5. SAVE (Async with Timeout)
			if spy.statusCode == http.StatusOK {
				go func(k string, data []byte) {
					ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
					defer cancel()
					
					if err := rdb.Set(ctx, k, data, time.Hour); err != nil {
						log.Printf("⚠️ [CACHE] Failed to save: %v", err)
					} else {
						log.Printf("💾 [CACHE] Saved key %s", k[:8])
					}
				}(key, spy.body.Bytes())
			}
		})
	}
}