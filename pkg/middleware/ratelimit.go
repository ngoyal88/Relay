package middleware

import (
	"net/http"
	"golang.org/x/time/rate"
)

// NewRateLimiter creates a middleware that limits requests.
// It returns a FUNCTION that wraps the next handler.
func NewRateLimiter(rps rate.Limit, burst int) func(http.Handler) http.Handler {
	// 1. Create the limiter instance (Closure)
	// This 'limiter' variable lives as long as the server runs.
	limiter := rate.NewLimiter(rps, burst)

	// 2. Return the Middleware function
	return func(next http.Handler) http.Handler {
		// 3. Return the actual Handler logic
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			
			// LOGIC: Check the limiter
			if !limiter.Allow() {
				http.Error(w, "Too Many Requests", http.StatusTooManyRequests)
				// STOP! Do not call next.ServeHTTP
				return
			}

			// SUCCESS: Call the next handler in the chain
			next.ServeHTTP(w, r)
		})
	}
}