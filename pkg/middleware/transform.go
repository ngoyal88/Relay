package middleware

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"regexp"
	"strings"
)

// TransformConfig defines transformation rules
type TransformConfig struct {
	// Header transformations
	RemoveHeaders     []string          `mapstructure:"remove_headers"`
	AddHeaders        map[string]string `mapstructure:"add_headers"`
	ReplaceHeaders    map[string]string `mapstructure:"replace_headers"`
	
	// Request body transformations
	RequestRules      []TransformRule   `mapstructure:"request_rules"`
	
	// Response body transformations
	ResponseRules     []TransformRule   `mapstructure:"response_rules"`
	
	// Content filtering
	MaskSensitiveData bool              `mapstructure:"mask_sensitive_data"`
	AllowedPaths      []string          `mapstructure:"allowed_paths"`
	BlockedPaths      []string          `mapstructure:"blocked_paths"`
}

// TransformRule defines a single transformation
type TransformRule struct {
	Type      string                 `mapstructure:"type"` // "add", "remove", "replace", "mask"
	Path      string                 `mapstructure:"path"` // JSON path (e.g., "messages[0].content")
	Value     interface{}            `mapstructure:"value"`
	Pattern   string                 `mapstructure:"pattern"` // For regex replace/mask
	Replace   string                 `mapstructure:"replace"`
}

// TransformMiddleware applies transformations to requests and responses
func TransformMiddleware(config TransformConfig) func(http.Handler) http.Handler {
	// Compile regex patterns
	allowedPatterns := compilePatterns(config.AllowedPaths)
	blockedPatterns := compilePatterns(config.BlockedPaths)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Check path filtering
			if !isPathAllowed(r.URL.Path, allowedPatterns, blockedPatterns) {
				http.Error(w, "Path not allowed", http.StatusForbidden)
				return
			}

			// Transform request headers
			transformHeaders(r.Header, config)

			// Transform request body
			if r.Method == http.MethodPost || r.Method == http.MethodPut {
				if err := transformRequestBody(r, config); err != nil {
					http.Error(w, "Request transformation failed", http.StatusBadRequest)
					return
				}
			}

			// Wrap response writer to transform response
			wrapper := &transformResponseWrapper{
				ResponseWriter: w,
				config:         config,
			}

			next.ServeHTTP(wrapper, r)
		})
	}
}

// transformHeaders modifies request headers
func transformHeaders(headers http.Header, config TransformConfig) {
	// Remove headers
	for _, header := range config.RemoveHeaders {
		headers.Del(header)
	}

	// Add headers
	for key, value := range config.AddHeaders {
		headers.Add(key, value)
	}

	// Replace headers
	for key, value := range config.ReplaceHeaders {
		headers.Set(key, value)
	}
}

// transformRequestBody applies transformations to request body
func transformRequestBody(r *http.Request, config TransformConfig) error {
	if len(config.RequestRules) == 0 && !config.MaskSensitiveData {
		return nil
	}

	// Read body
	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		return err
	}
	defer r.Body.Close()

	// Parse as JSON
	var data map[string]interface{}
	if err := json.Unmarshal(bodyBytes, &data); err != nil {
		// Not JSON, restore original body
		r.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))
		return nil
	}

	// Apply transformation rules
	for _, rule := range config.RequestRules {
		applyRule(data, rule)
	}

	// Mask sensitive data
	if config.MaskSensitiveData {
		maskSensitiveFields(data)
	}

	// Marshal back to JSON
	transformed, err := json.Marshal(data)
	if err != nil {
		return err
	}

	// Replace body
	r.Body = io.NopCloser(bytes.NewBuffer(transformed))
	r.ContentLength = int64(len(transformed))

	return nil
}

// applyRule applies a single transformation rule
func applyRule(data map[string]interface{}, rule TransformRule) {
	switch rule.Type {
	case "add":
		setValueAtPath(data, rule.Path, rule.Value)
	case "remove":
		deleteValueAtPath(data, rule.Path)
	case "replace":
		if existsAtPath(data, rule.Path) {
			setValueAtPath(data, rule.Path, rule.Value)
		}
	case "mask":
		if val := getValueAtPath(data, rule.Path); val != nil {
			if str, ok := val.(string); ok {
				masked := maskString(str, rule.Pattern)
				setValueAtPath(data, rule.Path, masked)
			}
		}
	}
}

// maskSensitiveFields automatically masks common sensitive data
func maskSensitiveFields(data map[string]interface{}) {
	sensitivePatterns := map[string]*regexp.Regexp{
		"email":        regexp.MustCompile(`[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}`),
		"phone":        regexp.MustCompile(`\b\d{3}[-.]?\d{3}[-.]?\d{4}\b`),
		"ssn":          regexp.MustCompile(`\b\d{3}-\d{2}-\d{4}\b`),
		"credit_card":  regexp.MustCompile(`\b\d{4}[-\s]?\d{4}[-\s]?\d{4}[-\s]?\d{4}\b`),
		"api_key":      regexp.MustCompile(`\b[a-zA-Z0-9_-]{32,}\b`),
	}

	maskRecursive(data, sensitivePatterns)
}

// maskRecursive recursively masks sensitive data
func maskRecursive(data interface{}, patterns map[string]*regexp.Regexp) {
	switch v := data.(type) {
	case map[string]interface{}:
		for key, value := range v {
			// Check if key suggests sensitive data
			keyLower := strings.ToLower(key)
			if strings.Contains(keyLower, "password") ||
			   strings.Contains(keyLower, "secret") ||
			   strings.Contains(keyLower, "token") ||
			   strings.Contains(keyLower, "key") {
				v[key] = "***MASKED***"
				continue
			}
			maskRecursive(value, patterns)
		}
	case []interface{}:
		for _, item := range v {
			maskRecursive(item, patterns)
		}
	case string:
		// Apply pattern matching
		for _, pattern := range patterns {
			if pattern.MatchString(v) {
				// This modifies by reference, so it works
				data = pattern.ReplaceAllString(v, "***MASKED***")
			}
		}
	}
}

// JSON path helpers (simplified implementation)
func getValueAtPath(data map[string]interface{}, path string) interface{} {
	parts := strings.Split(path, ".")
	current := interface{}(data)

	for _, part := range parts {
		switch v := current.(type) {
		case map[string]interface{}:
			current = v[part]
		default:
			return nil
		}
	}
	return current
}

func setValueAtPath(data map[string]interface{}, path string, value interface{}) {
	parts := strings.Split(path, ".")
	current := data

	for i, part := range parts {
		if i == len(parts)-1 {
			current[part] = value
		} else {
			if _, ok := current[part]; !ok {
				current[part] = make(map[string]interface{})
			}
			if next, ok := current[part].(map[string]interface{}); ok {
				current = next
			}
		}
	}
}

func deleteValueAtPath(data map[string]interface{}, path string) {
	parts := strings.Split(path, ".")
	current := data

	for i, part := range parts {
		if i == len(parts)-1 {
			delete(current, part)
		} else {
			if next, ok := current[part].(map[string]interface{}); ok {
				current = next
			} else {
				return
			}
		}
	}
}

func existsAtPath(data map[string]interface{}, path string) bool {
	return getValueAtPath(data, path) != nil
}

func maskString(s string, pattern string) string {
	if pattern == "" {
		// Default masking: show first and last 4 chars
		if len(s) <= 8 {
			return "***"
		}
		return s[:4] + "***" + s[len(s)-4:]
	}

	re := regexp.MustCompile(pattern)
	return re.ReplaceAllString(s, "***")
}

// Path filtering helpers
func compilePatterns(patterns []string) []*regexp.Regexp {
	result := make([]*regexp.Regexp, 0, len(patterns))
	for _, pattern := range patterns {
		if re, err := regexp.Compile(pattern); err == nil {
			result = append(result, re)
		}
	}
	return result
}

func isPathAllowed(path string, allowed, blocked []*regexp.Regexp) bool {
	// If no patterns, allow all
	if len(allowed) == 0 && len(blocked) == 0 {
		return true
	}

	// Check blocked first
	for _, pattern := range blocked {
		if pattern.MatchString(path) {
			return false
		}
	}

	// If allowed list exists, must match
	if len(allowed) > 0 {
		for _, pattern := range allowed {
			if pattern.MatchString(path) {
				return true
			}
		}
		return false
	}

	return true
}

// transformResponseWrapper wraps response writer to transform responses
type transformResponseWrapper struct {
	http.ResponseWriter
	config TransformConfig
	body   bytes.Buffer
}

func (w *transformResponseWrapper) Write(b []byte) (int, error) {
	// Capture response body
	w.body.Write(b)

	// Parse and transform if JSON
	if strings.Contains(w.Header().Get("Content-Type"), "application/json") {
		var data map[string]interface{}
		if err := json.Unmarshal(w.body.Bytes(), &data); err == nil {
			// Apply response rules
			for _, rule := range w.config.ResponseRules {
				applyRule(data, rule)
			}

			// Mask sensitive data
			if w.config.MaskSensitiveData {
				maskSensitiveFields(data)
			}

			// Write transformed response
			transformed, err := json.Marshal(data)
			if err == nil {
				return w.ResponseWriter.Write(transformed)
			}
		}
	}

	// Write original if transformation failed
	return w.ResponseWriter.Write(b)
}