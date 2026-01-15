package keymanager

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"time"

	"github.com/ngoyal88/relay/pkg/cache"
	"github.com/ngoyal88/relay/pkg/middleware"
)

// Manager handles API key operations
type Manager struct {
	rdb *cache.Client
}

// New creates a new key manager
func New(rdb *cache.Client) *Manager {
	return &Manager{rdb: rdb}
}

// CreateKey generates a new API key
func (m *Manager) CreateKey(ctx context.Context, name, userID, description string, rateLimit float64, burst int, quota int64, expiresIn *time.Duration) (*middleware.APIKey, error) {
	// Generate secure random key
	keyStr, err := generateSecureKey()
	if err != nil {
		return nil, fmt.Errorf("failed to generate key: %w", err)
	}

	now := time.Now()
	var expiresAt *time.Time
	if expiresIn != nil {
		exp := now.Add(*expiresIn)
		expiresAt = &exp
	}

	apiKey := &middleware.APIKey{
		Key:         keyStr,
		Name:        name,
		UserID:      userID,
		RateLimit:   rateLimit,
		Burst:       burst,
		Quota:       quota,
		Used:        0,
		Active:      true,
		CreatedAt:   now,
		ExpiresAt:   expiresAt,
		Description: description,
	}

	// Store in Redis
	keyData := fmt.Sprintf("apikey:%s", keyStr)
	data, err := json.Marshal(apiKey)
	if err != nil {
		return nil, err
	}

	if err := m.rdb.Set(ctx, keyData, data, 0); err != nil {
		return nil, err
	}

	// Also store in user index for listing
	userKeyList := fmt.Sprintf("user:%s:keys", userID)
	m.rdb.Redis().SAdd(ctx, userKeyList, keyStr)

	return apiKey, nil
}

// GetKey retrieves an API key
func (m *Manager) GetKey(ctx context.Context, key string) (*middleware.APIKey, error) {
	keyData := fmt.Sprintf("apikey:%s", key)
	data, err := m.rdb.Get(ctx, keyData)
	if err != nil {
		return nil, err
	}

	var apiKey middleware.APIKey
	if err := json.Unmarshal(data, &apiKey); err != nil {
		return nil, err
	}

	return &apiKey, nil
}

// UpdateKey updates an existing API key
func (m *Manager) UpdateKey(ctx context.Context, key string, updates map[string]interface{}) error {
	// Get existing key
	apiKey, err := m.GetKey(ctx, key)
	if err != nil {
		return err
	}

	// Apply updates
	if name, ok := updates["name"].(string); ok {
		apiKey.Name = name
	}
	if rateLimit, ok := updates["rate_limit"].(float64); ok {
		apiKey.RateLimit = rateLimit
	}
	if burst, ok := updates["burst"].(int); ok {
		apiKey.Burst = burst
	}
	if quota, ok := updates["quota"].(int64); ok {
		apiKey.Quota = quota
	}
	if active, ok := updates["active"].(bool); ok {
		apiKey.Active = active
	}
	if desc, ok := updates["description"].(string); ok {
		apiKey.Description = desc
	}

	// Save back
	keyData := fmt.Sprintf("apikey:%s", key)
	data, err := json.Marshal(apiKey)
	if err != nil {
		return err
	}

	return m.rdb.Set(ctx, keyData, data, 0)
}

// RevokeKey deactivates an API key
func (m *Manager) RevokeKey(ctx context.Context, key string) error {
	return m.UpdateKey(ctx, key, map[string]interface{}{
		"active": false,
	})
}

// DeleteKey permanently removes an API key
func (m *Manager) DeleteKey(ctx context.Context, key string) error {
	// Get key first to find user
	apiKey, err := m.GetKey(ctx, key)
	if err != nil {
		return err
	}

	// Remove from user's key list
	userKeyList := fmt.Sprintf("user:%s:keys", apiKey.UserID)
	m.rdb.Redis().SRem(ctx, userKeyList, key)

	// Delete the key
	keyData := fmt.Sprintf("apikey:%s", key)
	return m.rdb.Redis().Del(ctx, keyData).Err()
}

// ListUserKeys returns all keys for a user
func (m *Manager) ListUserKeys(ctx context.Context, userID string) ([]*middleware.APIKey, error) {
	userKeyList := fmt.Sprintf("user:%s:keys", userID)
	keys, err := m.rdb.Redis().SMembers(ctx, userKeyList).Result()
	if err != nil {
		return nil, err
	}

	result := make([]*middleware.APIKey, 0, len(keys))
	for _, key := range keys {
		apiKey, err := m.GetKey(ctx, key)
		if err == nil {
			result = append(result, apiKey)
		}
	}

	return result, nil
}

// RotateKey generates a new key and deactivates the old one
func (m *Manager) RotateKey(ctx context.Context, oldKey string) (*middleware.APIKey, error) {
	// Get old key details
	apiKey, err := m.GetKey(ctx, oldKey)
	if err != nil {
		return nil, err
	}

	// Create new key with same settings
	var expiresIn *time.Duration
	if apiKey.ExpiresAt != nil {
		remaining := time.Until(*apiKey.ExpiresAt)
		expiresIn = &remaining
	}

	rotatedFrom := oldKey
	if len(rotatedFrom) > 16 {
		rotatedFrom = rotatedFrom[:16] + "..."
	}

	newKey, err := m.CreateKey(
		ctx,
		apiKey.Name,
		apiKey.UserID,
		fmt.Sprintf("Rotated from %s", rotatedFrom),
		apiKey.RateLimit,
		apiKey.Burst,
		apiKey.Quota,
		expiresIn,
	)
	if err != nil {
		return nil, err
	}

	// Deactivate old key
	m.RevokeKey(ctx, oldKey)

	return newKey, nil
}

// generateSecureKey creates a cryptographically secure random key
func generateSecureKey() (string, error) {
	// Generate 32 bytes of random data
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}

	// Encode to base64 and add prefix
	encoded := base64.RawURLEncoding.EncodeToString(b)
	return fmt.Sprintf("relay_%s", encoded), nil
}
