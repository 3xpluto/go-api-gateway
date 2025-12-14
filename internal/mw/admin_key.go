package mw

import (
	"encoding/json"
	"net/http"
)

const AdminKeyHeader = "X-Admin-Key"

func RequireAdminKey(adminKey string, next http.Handler) http.Handler {
	// If no key configured, do not expose admin endpoints at all.
	if adminKey == "" {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.NotFound(w, r)
		})
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get(AdminKeyHeader) != adminKey {
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": "unauthorized"})
			return
		}
		next.ServeHTTP(w, r)
	})
}
