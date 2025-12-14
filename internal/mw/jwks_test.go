package mw

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func TestJWKSValidator_ValidToken(t *testing.T) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	kid := "kid1"

	jwks := map[string]any{
		"keys": []any{
			map[string]any{
				"kty": "RSA",
				"kid": kid,
				"use": "sig",
				"alg": "RS256",
				"n":   base64.RawURLEncoding.EncodeToString(priv.PublicKey.N.Bytes()),
				"e":   base64.RawURLEncoding.EncodeToString([]byte{1, 0, 1}),
			},
		},
	}

	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(jwks)
	}))
	defer s.Close()

	v, err := NewJWKSValidator(s.URL, JWKSValidatorOptions{
		HTTPTimeout: 2 * time.Second,
		CacheTTL:    5 * time.Minute,
		Leeway:      30 * time.Second,
		Issuers:     []string{"issuer-1"},
		Audiences:   []string{"apigw"},
		ValidAlgs:   []string{"RS256"},
	})
	if err != nil {
		t.Fatal(err)
	}

	claims := jwt.MapClaims{
		"sub": "user_123",
		"iss": "issuer-1",
		"aud": "apigw",
		"iat": time.Now().Unix(),
		"nbf": time.Now().Add(-5 * time.Second).Unix(),
		"exp": time.Now().Add(1 * time.Hour).Unix(),
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	tok.Header["kid"] = kid
	tokStr, err := tok.SignedString(priv)
	if err != nil {
		t.Fatal(err)
	}

	sub, err := v.Validate(context.Background(), tokStr)
	if err != nil {
		t.Fatalf("expected ok, got err: %v", err)
	}
	if sub != "user_123" {
		t.Fatalf("expected sub user_123, got %q", sub)
	}
}

func TestJWKSValidator_IssuerMismatch(t *testing.T) {
	priv, _ := rsa.GenerateKey(rand.Reader, 2048)
	kid := "kid1"
	jwks := map[string]any{
		"keys": []any{
			map[string]any{
				"kty": "RSA",
				"kid": kid,
				"n":   base64.RawURLEncoding.EncodeToString(priv.PublicKey.N.Bytes()),
				"e":   base64.RawURLEncoding.EncodeToString([]byte{1, 0, 1}),
			},
		},
	}
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(jwks)
	}))
	defer s.Close()

	v, _ := NewJWKSValidator(s.URL, JWKSValidatorOptions{Issuers: []string{"issuer-1"}})

	claims := jwt.MapClaims{
		"sub": "user_123",
		"iss": "other",
		"exp": time.Now().Add(1 * time.Hour).Unix(),
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	tok.Header["kid"] = kid
	tokStr, _ := tok.SignedString(priv)

	_, err := v.Validate(context.Background(), tokStr)
	if err == nil {
		t.Fatalf("expected error")
	}
}

func TestJWKSValidator_AudienceMismatch(t *testing.T) {
	priv, _ := rsa.GenerateKey(rand.Reader, 2048)
	kid := "kid1"
	jwks := map[string]any{
		"keys": []any{
			map[string]any{
				"kty": "RSA",
				"kid": kid,
				"n":   base64.RawURLEncoding.EncodeToString(priv.PublicKey.N.Bytes()),
				"e":   base64.RawURLEncoding.EncodeToString([]byte{1, 0, 1}),
			},
		},
	}
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(jwks)
	}))
	defer s.Close()

	v, _ := NewJWKSValidator(s.URL, JWKSValidatorOptions{Audiences: []string{"apigw"}})

	claims := jwt.MapClaims{
		"sub": "user_123",
		"aud": "nope",
		"exp": time.Now().Add(1 * time.Hour).Unix(),
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	tok.Header["kid"] = kid
	tokStr, _ := tok.SignedString(priv)

	_, err := v.Validate(context.Background(), tokStr)
	if err == nil {
		t.Fatalf("expected error")
	}
}
