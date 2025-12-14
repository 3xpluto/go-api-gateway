package mw

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/golang-jwt/jwt/v5"
)

type subjectKeyType string

const subjectKey subjectKeyType = "sub"

type Authenticator struct {
	Mode       string // "hmac"
	HMACSecret []byte
}

func (a Authenticator) ValidateBearer(r *http.Request) (string, error) {
	authz := r.Header.Get("Authorization")
	if authz == "" || !strings.HasPrefix(authz, "Bearer ") {
		return "", errors.New("missing bearer token")
	}
	tokStr := strings.TrimSpace(strings.TrimPrefix(authz, "Bearer "))

	tok, err := jwt.Parse(tokStr, func(token *jwt.Token) (any, error) {
		if a.Mode != "hmac" {
			return nil, errors.New("unsupported auth mode")
		}
		if token.Method.Alg() != jwt.SigningMethodHS256.Alg() {
			return nil, errors.New("unexpected jwt alg")
		}
		return a.HMACSecret, nil
	})
	if err != nil || !tok.Valid {
		return "", errors.New("invalid token")
	}
	claims, ok := tok.Claims.(jwt.MapClaims)
	if !ok {
		return "", errors.New("invalid claims")
	}
	sub, _ := claims["sub"].(string)
	if sub == "" {
		return "", errors.New("missing sub")
	}
	return sub, nil
}

func WithSubject(next http.Handler, sub string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := context.WithValue(r.Context(), subjectKey, sub)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func Subject(ctx context.Context) (string, bool) {
	v, ok := ctx.Value(subjectKey).(string)
	return v, ok
}
