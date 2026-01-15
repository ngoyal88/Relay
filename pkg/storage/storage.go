package storage

import (
	"context"
	"time"
)

// Store defines the interface for persisting data
type Store interface {
	// Request Logs
	SaveRequestLog(ctx context.Context, log *RequestLog) error
	GetRequestLog(ctx context.Context, id string) (*RequestLog, error)
	ListRequestLogs(ctx context.Context, filters LogFilters) ([]*RequestLog, error)

	// Analytics
	GetUsageStats(ctx context.Context, userID string, from, to time.Time) (*UsageStats, error)
	GetCostStats(ctx context.Context, userID string, from, to time.Time) (*CostStats, error)

	// Health check
	Ping(ctx context.Context) error
}

// LogFilters for querying request logs
type LogFilters struct {
	UserID     string
	APIKey     string
	From       time.Time
	To         time.Time
	StatusCode int
	Model      string
	Limit      int
	Offset     int
}

// UsageStats aggregated usage statistics
type UsageStats struct {
	TotalRequests int64            `json:"total_requests"`
	CacheHits     int64            `json:"cache_hits"`
	CacheMisses   int64            `json:"cache_misses"`
	ByModel       map[string]int64 `json:"by_model"`
	ByStatusCode  map[int]int64    `json:"by_status_code"`
	AvgDuration   time.Duration    `json:"avg_duration"`
}

// CostStats aggregated cost statistics
type CostStats struct {
	TotalCost   float64            `json:"total_cost"`
	TotalTokens int64              `json:"total_tokens"`
	ByModel     map[string]float64 `json:"by_model"`
}
