package main

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math/big"
	"net/http"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

type jwksDoc struct {
	Keys []jwkKey `json:"keys"`
}

type jwkKey struct {
	Kty string `json:"kty"`
	Kid string `json:"kid"`
	Use string `json:"use"`
	Alg string `json:"alg"`
	N   string `json:"n"`
	E   string `json:"e"`
}

func main() {
	var addr string
	var issuer string
	var audience string
	flag.StringVar(&addr, "addr", ":9009", "listen address")
	flag.StringVar(&issuer, "iss", "http://127.0.0.1:9009", "issuer claim")
	flag.StringVar(&audience, "aud", "apigw", "audience claim")
	flag.Parse()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		log.Fatal(err)
	}
	kid := randomKid()

	jwks := jwksDoc{Keys: []jwkKey{{
		Kty: "RSA",
		Kid: kid,
		Use: "sig",
		Alg: "RS256",
		N:   base64.RawURLEncoding.EncodeToString(key.PublicKey.N.Bytes()),
		E:   base64.RawURLEncoding.EncodeToString(intToBytes(key.PublicKey.E)),
	}}}

	mux := http.NewServeMux()

	mux.HandleFunc("/.well-known/jwks.json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(jwks)
	})

	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		sub := r.URL.Query().Get("sub")
		if sub == "" {
			sub = "user_123"
		}
		claims := jwt.MapClaims{
			"sub": sub,
			"iss": issuer,
			"aud": audience,
			"iat": time.Now().Unix(),
			"nbf": time.Now().Add(-5 * time.Second).Unix(),
			"exp": time.Now().Add(24 * time.Hour).Unix(),
		}
		tok := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
		tok.Header["kid"] = kid
		s, err := tok.SignedString(key)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(err.Error()))
			return
		}
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte(s))
	})

	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok"))
	})

	log.Printf("jwksmock listening on %s (issuer=%s aud=%s)", addr, issuer, audience)
	log.Printf("jwks url: %s/.well-known/jwks.json", issuer)
	log.Printf("token url: %s/token?sub=user_123", issuer)

	srv := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	log.Fatal(srv.ListenAndServe())
}

func intToBytes(v int) []byte {
	b := big.NewInt(int64(v)).Bytes()
	if len(b) == 0 {
		return []byte{0}
	}
	return b
}

func randomKid() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return fmt.Sprintf("%x", b)
}
