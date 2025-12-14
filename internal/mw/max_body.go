package mw

import (
	"encoding/json"
	"net/http"
)

func MaxBodyBytes(limit int64, next http.Handler) http.Handler {
	if limit <= 0 {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Fast fail when Content-Length is known.
		if r.ContentLength > limit && r.ContentLength != -1 {
			w.WriteHeader(http.StatusRequestEntityTooLarge)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"error":     "request_too_large",
				"max_bytes": limit,
			})
			return
		}

		// Safety net for unknown lengths (chunked). ReverseProxy will surface errors if exceeded.
		r.Body = http.MaxBytesReader(w, r.Body, limit)
		next.ServeHTTP(w, r)
	})
}
