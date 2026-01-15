package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/ngoyal88/relay/pkg/cache"
	"github.com/redis/go-redis/v9"
)

// RedisStore implements Store using Redis with time-series data
type RedisStore struct {
	rdb *cache.Client
	ttl time.Duration // How long to keep logs (e.g., 30 days)
}

// NewRedisStore creates a new Redis-backed storage
func NewRedisStore(rdb *cache.Client, logRetention time.Duration) *RedisStore {
	if logRetention == 0 {
		logRetention = 30 * 24 * time.Hour // Default 30 days
	}
	return &RedisStore{
		rdb: rdb,
		ttl: logRetention,
	}
}

// SaveRequestLog stores a request log in Redis
func (s *RedisStore) SaveRequestLog(ctx context.Context, log *RequestLog) error {
	// Store full log by ID
	key := fmt.Sprintf("log:%s", log.ID)
	data, err := json.Marshal(log)
	if err != nil {
		return err
	}

	if err := s.rdb.Set(ctx, key, data, s.ttl); err != nil {
		return err
	}

	// Add to time-series index
	timestamp := float64(log.Timestamp.Unix())
	cutoff := fmt.Sprintf("%f", float64(time.Now().Add(-s.ttl).Unix()))

	// Global timeline
	timelineKey := "logs:timeline"
	s.rdb.Redis().ZAdd(ctx, timelineKey, redis.Z{
		Score:  timestamp,
		Member: log.ID,
	})
	s.rdb.Redis().ZRemRangeByScore(ctx, timelineKey, "-inf", cutoff)
	s.rdb.Redis().Expire(ctx, timelineKey, s.ttl)

	// Per-user timeline
	if log.UserID != "" {
		userTimeline := fmt.Sprintf("logs:user:%s", log.UserID)
		s.rdb.Redis().ZAdd(ctx, userTimeline, redis.Z{
			Score:  timestamp,
			Member: log.ID,
		})
		s.rdb.Redis().ZRemRangeByScore(ctx, userTimeline, "-inf", cutoff)
		s.rdb.Redis().Expire(ctx, userTimeline, s.ttl)
	}

	// Per-model index
	if log.Model != "" {
		modelIndex := fmt.Sprintf("logs:model:%s", log.Model)
		s.rdb.Redis().ZAdd(ctx, modelIndex, redis.Z{
			Score:  timestamp,
			Member: log.ID,
		})
		s.rdb.Redis().ZRemRangeByScore(ctx, modelIndex, "-inf", cutoff)
		s.rdb.Redis().Expire(ctx, modelIndex, s.ttl)
	}

	return nil
}

// GetRequestLog retrieves a single log by ID
func (s *RedisStore) GetRequestLog(ctx context.Context, id string) (*RequestLog, error) {
	key := fmt.Sprintf("log:%s", id)
	data, err := s.rdb.Get(ctx, key)
	if err != nil {
		return nil, err
	}

	var log RequestLog
	if err := json.Unmarshal(data, &log); err != nil {
		return nil, err
	}

	return &log, nil
}

// ListRequestLogs queries logs with filters
func (s *RedisStore) ListRequestLogs(ctx context.Context, filters LogFilters) ([]*RequestLog, error) {
	// Determine which index to use
	var indexKey string
	if filters.UserID != "" {
		indexKey = fmt.Sprintf("logs:user:%s", filters.UserID)
	} else if filters.Model != "" {
		indexKey = fmt.Sprintf("logs:model:%s", filters.Model)
	} else {
		indexKey = "logs:timeline"
	}

	// Query by time range
	minScore := float64(filters.From.Unix())
	maxScore := float64(filters.To.Unix())
	if filters.To.IsZero() {
		maxScore = float64(time.Now().Unix())
	}

	// Get IDs from sorted set
	limit := filters.Limit
	if limit == 0 {
		limit = 100 // Default limit
	}

	ids, err := s.rdb.Redis().ZRevRangeByScore(ctx, indexKey, &redis.ZRangeBy{
		Min:    fmt.Sprintf("%f", minScore),
		Max:    fmt.Sprintf("%f", maxScore),
		Offset: int64(filters.Offset),
		Count:  int64(limit),
	}).Result()

	if err != nil {
		return nil, err
	}

	// Fetch full logs
	logs := make([]*RequestLog, 0, len(ids))
	for _, id := range ids {
		log, err := s.GetRequestLog(ctx, id)
		if err == nil {
			// Apply additional filters
			if filters.StatusCode != 0 && log.StatusCode != filters.StatusCode {
				continue
			}
			logs = append(logs, log)
		}
	}

	return logs, nil
}

// GetUsageStats calculates usage statistics
func (s *RedisStore) GetUsageStats(ctx context.Context, userID string, from, to time.Time) (*UsageStats, error) {
	logs, err := s.ListRequestLogs(ctx, LogFilters{
		UserID: userID,
		From:   from,
		To:     to,
		Limit:  10000, // Get all logs in range
	})
	if err != nil {
		return nil, err
	}

	stats := &UsageStats{
		ByModel:      make(map[string]int64),
		ByStatusCode: make(map[int]int64),
	}

	var totalDuration time.Duration
	for _, log := range logs {
		stats.TotalRequests++

		if log.CacheHit {
			stats.CacheHits++
		} else {
			stats.CacheMisses++
		}

		if log.Model != "" {
			stats.ByModel[log.Model]++
		}

		stats.ByStatusCode[log.StatusCode]++
		totalDuration += log.Duration
	}

	if stats.TotalRequests > 0 {
		stats.AvgDuration = totalDuration / time.Duration(stats.TotalRequests)
	}

	return stats, nil
}

// GetCostStats calculates cost statistics
func (s *RedisStore) GetCostStats(ctx context.Context, userID string, from, to time.Time) (*CostStats, error) {
	logs, err := s.ListRequestLogs(ctx, LogFilters{
		UserID: userID,
		From:   from,
		To:     to,
		Limit:  10000,
	})
	if err != nil {
		return nil, err
	}

	stats := &CostStats{
		ByModel: make(map[string]float64),
	}

	for _, log := range logs {
		stats.TotalCost += log.CostUSD
		stats.TotalTokens += int64(log.TokensUsed)

		if log.Model != "" {
			stats.ByModel[log.Model] += log.CostUSD
		}
	}

	return stats, nil
}

// Ping checks Redis connection
func (s *RedisStore) Ping(ctx context.Context) error {
	return s.rdb.Redis().Ping(ctx).Err()
}
