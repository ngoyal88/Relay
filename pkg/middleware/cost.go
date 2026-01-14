package middleware

import (
	"bytes"
	"encoding/json"
	"io"
	"log"
	"net/http"
	
	"github.com/ngoyal88/relay/pkg/ai" // Import your new AI package
)

// OpenAIRequest mimics the structure of an incoming JSON payload
type OpenAIRequest struct {
	Model    string `json:"model"`
	Messages []struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	} `json:"messages"`
}

func TokenCostLogger(next http.Handler) http.Handler {
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

		// 3. PARSE & COUNT (Async - don't slow down the request)
		// We do this in a goroutine so the user request isn't delayed by token counting
		go func(data []byte) {
			var payload OpenAIRequest
			if err := json.Unmarshal(data, &payload); err != nil {
				// Not a valid OpenAI JSON? Maybe just a GET request. Ignore.
				return
			}

			// Combine all messages into one string to count
			fullText := ""
			for _, msg := range payload.Messages {
				fullText += msg.Content
			}

			// Count!
			count, _ := ai.CountTokens(payload.Model, fullText)
			cost := ai.EstimateCost(count, payload.Model)

			log.Printf("💰 [COST] Model: %s | Tokens: %d | Est. Cost: $%.6f", payload.Model, count, cost)
		}(bodyBytes)

		// 4. PROCEED
		next.ServeHTTP(w, r)
	})
}