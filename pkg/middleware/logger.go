package middleware

import (
	"log"
	"net/http"
	"time"
)

func RequestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		// Call the next handler (The Request happens here)
		next.ServeHTTP(w, r)

		// Logic runs AFTER the request is finished
		log.Printf(
			"[%s] %s %s -> %v",
			r.Method,
			r.URL.Path,
			r.RemoteAddr,
			time.Since(start),
		)
	})
}