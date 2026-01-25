package middleware

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/ngoyal88/relay/pkg/storage"
)

// RequestLoggingMiddleware logs requests into the configured store.
func RequestLoggingMiddleware(store storage.Store, enableLogging bool) func(http.Handler) http.Handler {
	if !enableLogging || store == nil {
		return func(next http.Handler) http.Handler { return next }
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()

			var requestBody map[string]interface{}
			if r.Method == http.MethodPost || r.Method == http.MethodPut {
				bodyBytes, _ := io.ReadAll(r.Body)
				r.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))

				if len(bodyBytes) > 0 {
					json.Unmarshal(bodyBytes, &requestBody)
				}
			}

			wrapper := &loggingResponseWrapper{
				ResponseWriter: w,
				statusCode:     http.StatusOK,
			}

			next.ServeHTTP(wrapper, r)

			var responseBody map[string]interface{}
			if wrapper.body.Len() > 0 {
				json.Unmarshal(wrapper.body.Bytes(), &responseBody)
			}

			var apiKeyStr, userID string
			if apiKey, ok := GetAPIKeyFromContext(r.Context()); ok {
				apiKeyStr = truncateKey(apiKey.Key)
				userID = apiKey.UserID
			}

			cacheHit := wrapper.Header().Get("X-Cache") == "HIT"
			model, _ := requestBody["model"].(string)

			entry := storage.RequestLog{
				ID:           generateLogID(),
				Timestamp:    start,
				Method:       r.Method,
				Path:         r.URL.Path,
				UserAgent:    r.UserAgent(),
				RemoteAddr:   r.RemoteAddr,
				APIKey:       apiKeyStr,
				UserID:       userID,
				RequestBody:  requestBody,
				ResponseBody: responseBody,
				StatusCode:   wrapper.statusCode,
				Duration:     time.Since(start),
				Model:        model,
				CacheHit:     cacheHit,
			}

			if tokens, ok := GetTokenCountFromContext(r.Context()); ok {
				entry.TokensUsed = tokens
			}

			if costUSD, ok := GetTokenCostFromContext(r.Context()); ok {
				entry.CostUSD = costUSD
			}

			go func(logEntry storage.RequestLog) {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()

				if err := store.SaveRequestLog(ctx, &logEntry); err != nil {
					log.Printf("[REQUEST LOG] failed to persist entry %s: %v", logEntry.ID, err)
				}
			}(entry)
		})
	}
}

type loggingResponseWrapper struct {
	http.ResponseWriter
	body       bytes.Buffer
	statusCode int
}

func (w *loggingResponseWrapper) WriteHeader(code int) {
	w.statusCode = code
	w.ResponseWriter.WriteHeader(code)
}

func (w *loggingResponseWrapper) Write(b []byte) (int, error) {
	// Copy to buffer
	w.body.Write(b)
	return w.ResponseWriter.Write(b)
}

func generateLogID() string {
	return fmt.Sprintf("log_%d", time.Now().UnixNano())
}

func truncateKey(key string) string {
	if len(key) == 0 {
		return ""
	}
	if len(key) <= 12 {
		return key
	}
	return key[:12] + "..."
}
