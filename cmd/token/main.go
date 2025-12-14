package main

import (
	"flag"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func main() {
	var secret string
	var sub string
	flag.StringVar(&secret, "secret", "dev-secret", "HS256 secret")
	flag.StringVar(&sub, "sub", "user_123", "subject claim")
	flag.Parse()

	claims := jwt.MapClaims{
		"sub": sub,
		"iat": time.Now().Unix(),
		"exp": time.Now().Add(24 * time.Hour).Unix(),
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	s, err := tok.SignedString([]byte(secret))
	if err != nil {
		panic(err)
	}
	fmt.Println(s)
}
