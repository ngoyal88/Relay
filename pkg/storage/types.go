package storage

import "time"

// RequestLog captures request/response details for persistence layers.
type RequestLog struct {
	ID           string                 `json:"id"`
	Timestamp    time.Time              `json:"timestamp"`
	Method       string                 `json:"method"`
	Path         string                 `json:"path"`
	UserAgent    string                 `json:"user_agent"`
	RemoteAddr   string                 `json:"remote_addr"`
	APIKey       string                 `json:"api_key,omitempty"`
	UserID       string                 `json:"user_id,omitempty"`
	RequestBody  map[string]interface{} `json:"request_body,omitempty"`
	ResponseBody map[string]interface{} `json:"response_body,omitempty"`
	StatusCode   int                    `json:"status_code"`
	Duration     time.Duration          `json:"duration"`
	TokensUsed   int                    `json:"tokens_used,omitempty"`
	Model        string                 `json:"model,omitempty"`
	CostUSD      float64                `json:"cost_usd,omitempty"`
	CacheHit     bool                   `json:"cache_hit"`
	Error        string                 `json:"error,omitempty"`
}
