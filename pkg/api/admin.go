package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/ngoyal88/relay/pkg/keymanager"
	"github.com/ngoyal88/relay/pkg/storage"
)

// AdminAPI provides endpoints for managing the relay
type AdminAPI struct {
	keyManager *keymanager.Manager
	store      storage.Store
	adminKey   string // Simple admin authentication
}

// NewAdminAPI creates a new admin API handler
func NewAdminAPI(km *keymanager.Manager, store storage.Store, adminKey string) *AdminAPI {
	return &AdminAPI{
		keyManager: km,
		store:      store,
		adminKey:   adminKey,
	}
}

// RegisterRoutes registers admin endpoints
func (api *AdminAPI) RegisterRoutes(mux *http.ServeMux) {
	// API Key Management
	mux.HandleFunc("/admin/keys", api.authenticate(api.handleKeys))
	mux.HandleFunc("/admin/keys/create", api.authenticate(api.handleCreateKey))
	mux.HandleFunc("/admin/keys/revoke", api.authenticate(api.handleRevokeKey))
	mux.HandleFunc("/admin/keys/delete", api.authenticate(api.handleDeleteKey))
	mux.HandleFunc("/admin/keys/rotate", api.authenticate(api.handleRotateKey))
	
	// Analytics
	mux.HandleFunc("/admin/usage", api.authenticate(api.handleUsageStats))
	mux.HandleFunc("/admin/costs", api.authenticate(api.handleCostStats))
	mux.HandleFunc("/admin/logs", api.authenticate(api.handleLogs))
	
	// System
	mux.HandleFunc("/admin/health", api.handleHealth)
}

// authenticate middleware checks admin key
func (api *AdminAPI) authenticate(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("X-Admin-Key")
		if authHeader != api.adminKey {
			respondJSON(w, http.StatusUnauthorized, map[string]string{
				"error": "Invalid admin key",
			})
			return
		}
		next(w, r)
	}
}

// handleKeys lists all API keys for a user
func (api *AdminAPI) handleKeys(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	userID := r.URL.Query().Get("user_id")
	if userID == "" {
		respondJSON(w, http.StatusBadRequest, map[string]string{
			"error": "user_id parameter required",
		})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	keys, err := api.keyManager.ListUserKeys(ctx, userID)
	if err != nil {
		respondJSON(w, http.StatusInternalServerError, map[string]string{
			"error": fmt.Sprintf("Failed to list keys: %v", err),
		})
		return
	}

	respondJSON(w, http.StatusOK, map[string]interface{}{
		"keys": keys,
	})
}

// handleCreateKey creates a new API key
func (api *AdminAPI) handleCreateKey(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Name        string  `json:"name"`
		UserID      string  `json:"user_id"`
		Description string  `json:"description"`
		RateLimit   float64 `json:"rate_limit"`
		Burst       int     `json:"burst"`
		Quota       int64   `json:"quota"`
		ExpiresInDays int   `json:"expires_in_days"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{
			"error": "Invalid request body",
		})
		return
	}

	// Validation
	if req.Name == "" || req.UserID == "" {
		respondJSON(w, http.StatusBadRequest, map[string]string{
			"error": "name and user_id are required",
		})
		return
	}

	var expiresIn *time.Duration
	if req.ExpiresInDays > 0 {
		duration := time.Duration(req.ExpiresInDays) * 24 * time.Hour
		expiresIn = &duration
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	apiKey, err := api.keyManager.CreateKey(
		ctx,
		req.Name,
		req.UserID,
		req.Description,
		req.RateLimit,
		req.Burst,
		req.Quota,
		expiresIn,
	)
	if err != nil {
		respondJSON(w, http.StatusInternalServerError, map[string]string{
			"error": fmt.Sprintf("Failed to create key: %v", err),
		})
		return
	}

	respondJSON(w, http.StatusCreated, map[string]interface{}{
		"api_key": apiKey,
		"message": "API key created successfully. Store it securely - it won't be shown again.",
	})
}

// handleRevokeKey deactivates an API key
func (api *AdminAPI) handleRevokeKey(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Key string `json:"key"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{
			"error": "Invalid request body",
		})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	if err := api.keyManager.RevokeKey(ctx, req.Key); err != nil {
		respondJSON(w, http.StatusInternalServerError, map[string]string{
			"error": fmt.Sprintf("Failed to revoke key: %v", err),
		})
		return
	}

	respondJSON(w, http.StatusOK, map[string]string{
		"message": "API key revoked successfully",
	})
}

// handleDeleteKey permanently removes an API key
func (api *AdminAPI) handleDeleteKey(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	key := r.URL.Query().Get("key")
	if key == "" {
		respondJSON(w, http.StatusBadRequest, map[string]string{
			"error": "key parameter required",
		})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	if err := api.keyManager.DeleteKey(ctx, key); err != nil {
		respondJSON(w, http.StatusInternalServerError, map[string]string{
			"error": fmt.Sprintf("Failed to delete key: %v", err),
		})
		return
	}

	respondJSON(w, http.StatusOK, map[string]string{
		"message": "API key deleted successfully",
	})
}

// handleRotateKey creates a new key and deactivates the old one
func (api *AdminAPI) handleRotateKey(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		OldKey string `json:"old_key"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{
			"error": "Invalid request body",
		})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	newKey, err := api.keyManager.RotateKey(ctx, req.OldKey)
	if err != nil {
		respondJSON(w, http.StatusInternalServerError, map[string]string{
			"error": fmt.Sprintf("Failed to rotate key: %v", err),
		})
		return
	}

	respondJSON(w, http.StatusOK, map[string]interface{}{
		"new_key": newKey,
		"message": "Key rotated successfully. Old key has been revoked.",
	})
}

// handleUsageStats returns usage statistics
func (api *AdminAPI) handleUsageStats(w http.ResponseWriter, r *http.Request) {
	if api.store == nil {
		respondJSON(w, http.StatusServiceUnavailable, map[string]string{
			"error": "Logging not enabled",
		})
		return
	}

	userID := r.URL.Query().Get("user_id")
	fromStr := r.URL.Query().Get("from")
	toStr := r.URL.Query().Get("to")

	from, _ := time.Parse(time.RFC3339, fromStr)
	to, _ := time.Parse(time.RFC3339, toStr)
	
	if to.IsZero() {
		to = time.Now()
	}
	if from.IsZero() {
		from = to.AddDate(0, 0, -7) // Last 7 days
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	stats, err := api.store.GetUsageStats(ctx, userID, from, to)
	if err != nil {
		respondJSON(w, http.StatusInternalServerError, map[string]string{
			"error": fmt.Sprintf("Failed to get stats: %v", err),
		})
		return
	}

	respondJSON(w, http.StatusOK, stats)
}

// handleCostStats returns cost statistics
func (api *AdminAPI) handleCostStats(w http.ResponseWriter, r *http.Request) {
	if api.store == nil {
		respondJSON(w, http.StatusServiceUnavailable, map[string]string{
			"error": "Logging not enabled",
		})
		return
	}

	userID := r.URL.Query().Get("user_id")
	fromStr := r.URL.Query().Get("from")
	toStr := r.URL.Query().Get("to")

	from, _ := time.Parse(time.RFC3339, fromStr)
	to, _ := time.Parse(time.RFC3339, toStr)
	
	if to.IsZero() {
		to = time.Now()
	}
	if from.IsZero() {
		from = to.AddDate(0, -1, 0) // Last month
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	stats, err := api.store.GetCostStats(ctx, userID, from, to)
	if err != nil {
		respondJSON(w, http.StatusInternalServerError, map[string]string{
			"error": fmt.Sprintf("Failed to get stats: %v", err),
		})
		return
	}

	respondJSON(w, http.StatusOK, stats)
}

// handleLogs returns request logs
func (api *AdminAPI) handleLogs(w http.ResponseWriter, r *http.Request) {
	if api.store == nil {
		respondJSON(w, http.StatusServiceUnavailable, map[string]string{
			"error": "Logging not enabled",
		})
		return
	}

	filters := storage.LogFilters{
		UserID:  r.URL.Query().Get("user_id"),
		Model:   r.URL.Query().Get("model"),
		Limit:   100,
	}

	fromStr := r.URL.Query().Get("from")
	toStr := r.URL.Query().Get("to")
	
	if fromStr != "" {
		filters.From, _ = time.Parse(time.RFC3339, fromStr)
	}
	if toStr != "" {
		filters.To, _ = time.Parse(time.RFC3339, toStr)
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	logs, err := api.store.ListRequestLogs(ctx, filters)
	if err != nil {
		respondJSON(w, http.StatusInternalServerError, map[string]string{
			"error": fmt.Sprintf("Failed to get logs: %v", err),
		})
		return
	}

	respondJSON(w, http.StatusOK, map[string]interface{}{
		"logs":  logs,
		"count": len(logs),
	})
}

// handleHealth returns system health
func (api *AdminAPI) handleHealth(w http.ResponseWriter, r *http.Request) {
	health := map[string]interface{}{
		"status": "healthy",
		"timestamp": time.Now(),
	}

	if api.store != nil {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		
		if err := api.store.Ping(ctx); err != nil {
			health["storage"] = "unhealthy"
			health["status"] = "degraded"
		} else {
			health["storage"] = "healthy"
		}
	}

	respondJSON(w, http.StatusOK, health)
}

func respondJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}