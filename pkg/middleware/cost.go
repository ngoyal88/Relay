package middleware

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"

	"github.com/ngoyal88/relay/pkg/ai"
	"github.com/ngoyal88/relay/pkg/config"
)

// OpenAIRequest mimics the structure of an incoming JSON payload
type OpenAIRequest struct {
	Model    string `json:"model"`
	Messages []struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	} `json:"messages"`
}

func TokenCostLogger(cfgStore *config.Store) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// 1. DRAIN THE BODY
			// We read all bytes from the request body into a byte array
			bodyBytes, err := io.ReadAll(r.Body)
			if err != nil {
				http.Error(w, "Failed to read body", http.StatusInternalServerError)
				return
			}

			// 2. REFILL THE BODY (Crucial Step!)
			// We create a new Buffer with the same bytes and assign it back to r.Body
			// NopCloser makes it look like a Closeable ReadCloser
			r.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))

			// 3. PARSE & COUNT (sync so values can be shared downstream)
			var payload OpenAIRequest
			if err := json.Unmarshal(bodyBytes, &payload); err == nil {
				if cfg := cfgStore.Get(); cfg != nil && len(cfg.Models) > 0 {
					fullText := ""
					for _, msg := range payload.Messages {
						fullText += msg.Content
					}

					count, _ := ai.CountTokens(payload.Model, fullText)
					cost := ai.EstimateCost(count, payload.Model, cfg.Models)

					ctx := context.WithValue(r.Context(), tokenCountContextKey, count)
					ctx = context.WithValue(ctx, tokenCostContextKey, cost)
					r = r.WithContext(ctx)

					requestTokenHistogram.Observe(float64(count))
					log.Printf("💰 [COST] Model: %s | Tokens: %d | Est. Cost: $%.6f", payload.Model, count, cost)
				}
			}

			// 4. PROCEED
			next.ServeHTTP(w, r)
		})
	}
}
