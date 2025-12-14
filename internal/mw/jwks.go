package mw

import (
	"context"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

type JWKSValidatorOptions struct {
	HTTPTimeout time.Duration
	CacheTTL    time.Duration
	Leeway      time.Duration

	// If provided, token must match one of these issuers.
	Issuers []string
	// If provided, token must match one of these audiences.
	Audiences []string

	// Allowed JWT algs (default ["RS256"])
	ValidAlgs []string
}

// JWKSValidator validates RS256 JWTs using a remote JWKS.
// It caches public keys by kid and refreshes on cache-expiry or unknown kid.
type JWKSValidator struct {
	url string

	client    *http.Client
	cacheTTL  time.Duration
	leeway    time.Duration
	validAlgs []string

	issuerSet map[string]struct{}
	audSet    map[string]struct{}

	mu        sync.RWMutex
	keys      map[string]*rsa.PublicKey
	fetchedAt time.Time

	refreshMu sync.Mutex
}

type jwksDoc struct {
	Keys []jwkKey `json:"keys"`
}

type jwkKey struct {
	Kid string `json:"kid"`
	Kty string `json:"kty"`
	Alg string `json:"alg"`
	Use string `json:"use"`

	N string `json:"n"`
	E string `json:"e"`
}

func NewJWKSValidator(url string, opts JWKSValidatorOptions) (*JWKSValidator, error) {
	if url == "" {
		return nil, errors.New("jwks url required")
	}

	timeout := opts.HTTPTimeout
	if timeout <= 0 {
		timeout = 3 * time.Second
	}
	ttl := opts.CacheTTL
	if ttl <= 0 {
		ttl = 5 * time.Minute
	}
	leeway := opts.Leeway
	if leeway < 0 {
		leeway = 0
	}

	validAlgs := opts.ValidAlgs
	if len(validAlgs) == 0 {
		validAlgs = []string{"RS256"}
	}

	issuerSet := map[string]struct{}{}
	for _, iss := range opts.Issuers {
		if iss != "" {
			issuerSet[iss] = struct{}{}
		}
	}

	audSet := map[string]struct{}{}
	for _, aud := range opts.Audiences {
		if aud != "" {
			audSet[aud] = struct{}{}
		}
	}

	v := &JWKSValidator{
		url: url,
		client: &http.Client{
			Timeout: timeout,
		},
		cacheTTL:  ttl,
		leeway:    leeway,
		validAlgs: validAlgs,
		issuerSet: issuerSet,
		audSet:    audSet,
		keys:      make(map[string]*rsa.PublicKey),
	}
	return v, nil
}

// Validate validates the JWT string, returning the "sub" on success.
func (j *JWKSValidator) Validate(ctx context.Context, tokenStr string) (string, error) {
	if tokenStr == "" {
		return "", errors.New("missing token")
	}

	claims := jwt.MapClaims{}
	parser := jwt.NewParser(
		jwt.WithValidMethods(j.validAlgs),
		jwt.WithoutClaimsValidation(), // we validate with leeway + multi-aud ourselves
	)

	tok, err := parser.ParseWithClaims(tokenStr, claims, func(t *jwt.Token) (any, error) {
		kid, _ := t.Header["kid"].(string)
		if kid == "" {
			return nil, errors.New("missing kid")
		}
		return j.getKey(ctx, kid)
	})
	if err != nil || tok == nil || !tok.Valid {
		return "", errors.New("invalid token")
	}

	if err := j.validateClaims(claims); err != nil {
		return "", err
	}

	sub, _ := claims["sub"].(string)
	if sub == "" {
		return "", errors.New("missing sub")
	}
	return sub, nil
}

func (j *JWKSValidator) validateClaims(claims jwt.MapClaims) error {
	now := time.Now().Unix()
	leeway := int64(j.leeway.Seconds())

	// iss
	if len(j.issuerSet) > 0 {
		iss, _ := claims["iss"].(string)
		if iss == "" {
			return errors.New("missing iss")
		}
		if _, ok := j.issuerSet[iss]; !ok {
			return errors.New("invalid issuer")
		}
	}

	// aud
	if len(j.audSet) > 0 {
		auds := extractAudiences(claims["aud"])
		if len(auds) == 0 {
			return errors.New("missing aud")
		}
		ok := false
		for _, a := range auds {
			if _, hit := j.audSet[a]; hit {
				ok = true
				break
			}
		}
		if !ok {
			return errors.New("invalid audience")
		}
	}

	// exp (required)
	exp, ok := extractInt64(claims["exp"])
	if !ok {
		return errors.New("missing exp")
	}
	if now > exp+leeway {
		return errors.New("token expired")
	}

	// nbf (optional)
	if nbf, ok := extractInt64(claims["nbf"]); ok {
		if now < nbf-leeway {
			return errors.New("token not active")
		}
	}

	// iat (optional) - not strictly required

	return nil
}

func extractAudiences(v any) []string {
	switch t := v.(type) {
	case string:
		if t == "" {
			return nil
		}
		return []string{t}
	case []any:
		out := make([]string, 0, len(t))
		for _, it := range t {
			if s, ok := it.(string); ok && s != "" {
				out = append(out, s)
			}
		}
		return out
	case []string:
		out := make([]string, 0, len(t))
		for _, s := range t {
			if s != "" {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

func extractInt64(v any) (int64, bool) {
	switch t := v.(type) {
	case float64:
		return int64(t), true
	case int64:
		return t, true
	case json.Number:
		i, err := t.Int64()
		return i, err == nil
	default:
		return 0, false
	}
}

func (j *JWKSValidator) getKey(ctx context.Context, kid string) (*rsa.PublicKey, error) {
	j.mu.RLock()
	key := j.keys[kid]
	fresh := time.Since(j.fetchedAt) < j.cacheTTL
	j.mu.RUnlock()
	if key != nil && fresh {
		return key, nil
	}

	// Refresh on unknown kid or stale cache
	if err := j.refresh(ctx); err != nil {
		// If we already have a key cached, allow using it even if stale.
		j.mu.RLock()
		key = j.keys[kid]
		j.mu.RUnlock()
		if key != nil {
			return key, nil
		}
		return nil, err
	}

	j.mu.RLock()
	key = j.keys[kid]
	j.mu.RUnlock()
	if key == nil {
		return nil, errors.New("unknown kid")
	}
	return key, nil
}

func (j *JWKSValidator) refresh(ctx context.Context) error {
	// serialize refresh to avoid stampede
	j.refreshMu.Lock()
	defer j.refreshMu.Unlock()

	// another goroutine may have refreshed while we waited
	j.mu.RLock()
	stillFresh := time.Since(j.fetchedAt) < j.cacheTTL
	j.mu.RUnlock()
	if stillFresh {
		return nil
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, j.url, nil)
	if err != nil {
		return err
	}
	resp, err := j.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("jwks http %d", resp.StatusCode)
	}

	var doc jwksDoc
	if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
		return err
	}
	if len(doc.Keys) == 0 {
		return errors.New("jwks empty")
	}

	next := make(map[string]*rsa.PublicKey, len(doc.Keys))
	for _, k := range doc.Keys {
		if k.Kid == "" {
			continue
		}
		if k.Kty != "RSA" {
			continue
		}
		// If alg is provided in JWKS, you may optionally enforce it here. We still enforce via parser valid methods.
		pub, err := jwkToRSAPublicKey(k)
		if err != nil {
			continue
		}
		next[k.Kid] = pub
	}
	if len(next) == 0 {
		return errors.New("jwks: no usable rsa keys")
	}

	j.mu.Lock()
	j.keys = next
	j.fetchedAt = time.Now()
	j.mu.Unlock()
	return nil
}

func jwkToRSAPublicKey(k jwkKey) (*rsa.PublicKey, error) {
	if k.N == "" || k.E == "" {
		return nil, errors.New("missing n/e")
	}

	nBytes, err := base64.RawURLEncoding.DecodeString(k.N)
	if err != nil {
		return nil, err
	}
	eBytes, err := base64.RawURLEncoding.DecodeString(k.E)
	if err != nil {
		return nil, err
	}

	n := new(big.Int).SetBytes(nBytes)
	e := new(big.Int).SetBytes(eBytes)

	if n.Sign() <= 0 || e.Sign() <= 0 {
		return nil, errors.New("bad rsa params")
	}
	if !e.IsInt64() {
		return nil, errors.New("rsa exponent too large")
	}
	return &rsa.PublicKey{N: n, E: int(e.Int64())}, nil
}
