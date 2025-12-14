package mw

import (
	"encoding/json"
	"net/http"
)

type AuthHandler interface {
	ValidateBearer(r *http.Request) (string, error)
}

func RequireAuth(auth AuthHandler, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sub, err := auth.ValidateBearer(r)
		if err != nil {
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"error": "unauthorized",
			})
			return
		}
		WithSubject(next, sub).ServeHTTP(w, r)
	})
}

func OptionalAuth(auth AuthHandler, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sub, err := auth.ValidateBearer(r)
		if err != nil {
			next.ServeHTTP(w, r)
			return
		}
		WithSubject(next, sub).ServeHTTP(w, r)
	})
}
