package ai

import (
	"github.com/pkoukk/tiktoken-go"
)

// CountTokens returns the number of tokens in a string for a specific model.
func CountTokens(model string, text string) (int, error) {
	// 1. Get the encoding for the model (e.g., gpt-4 uses 'cl100k_base')
	tkm, err := tiktoken.EncodingForModel(model)
	if err != nil {
		// Fallback to gpt-3.5 if model is unknown
		tkm, _ = tiktoken.GetEncoding("cl100k_base")
	}

	// 2. Encode and count
	tokenIds := tkm.Encode(text, nil, nil)
	return len(tokenIds), nil
}

// EstimateCost calculates price based on input tokens using provided price map (USD per 1k tokens)
func EstimateCost(tokens int, model string, prices map[string]float64) float64 {
	pricePer1k, ok := prices[model]
	if !ok {
		return 0
	}
	return (float64(tokens) / 1000.0) * pricePer1k
}
