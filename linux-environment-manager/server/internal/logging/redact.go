package logging

import (
	"regexp"
	"strings"
)

// redactPatterns defines patterns that should be redacted from log metadata.
var redactPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)(password|passwd|pwd)\s*[=:]\s*"?[^"\s,}]+"?`),
	regexp.MustCompile(`(?i)(token|access_token|refresh_token|api_key|apikey|api-key)\s*[=:]\s*"?[^"\s,}]+"?`),
	regexp.MustCompile(`(?i)(authorization)\s*[=:]\s*"?[^"\s,}]+"?`),
	regexp.MustCompile(`(?i)(secret|private_key|private-key)\s*[=:]\s*"?[^"\s,}]+"?`),
	regexp.MustCompile(`(?i)(cookie)\s*[=:]\s*"?[^"\s,}]+"?`),
	regexp.MustCompile(`(?i)(credential)\s*[=:]\s*"?[^"\s,}]+"?`),
}

// sensitiveKeys are JSON keys whose values should always be redacted.
var sensitiveKeys = map[string]bool{
	"password":        true,
	"passwd":          true,
	"pwd":             true,
	"token":           true,
	"access_token":    true,
	"refresh_token":   true,
	"api_key":         true,
	"apikey":          true,
	"api-key":         true,
	"authorization":   true,
	"secret":          true,
	"private_key":     true,
	"private-key":     true,
	"cookie":          true,
	"credential":      true,
	"enrollment_token": true,
}

// redactMetadata removes sensitive values from a metadata map.
// Returns the sanitized map and whether any changes were made.
func redactMetadata(m map[string]any) (map[string]any, bool) {
	if m == nil {
		return nil, false
	}
	changed := false
	result := make(map[string]any, len(m))
	for k, v := range m {
		if sensitiveKeys[strings.ToLower(k)] {
			result[k] = "[REDACTED]"
			changed = true
			continue
		}
		if s, ok := v.(string); ok {
			redacted := redactString(s)
			if redacted != s {
				result[k] = redacted
				changed = true
				continue
			}
		}
		result[k] = v
	}
	return result, changed
}

// redactString removes sensitive patterns from a string.
func redactString(s string) string {
	result := s
	for _, pattern := range redactPatterns {
		result = pattern.ReplaceAllString(result, "[REDACTED]")
	}
	return result
}

// containsSensitiveData checks if a value might contain secrets.
func containsSensitiveData(v any) bool {
	s, ok := v.(string)
	if !ok {
		return false
	}
	lower := strings.ToLower(s)
	for key := range sensitiveKeys {
		if strings.Contains(lower, key) {
			return true
		}
	}
	return false
}
