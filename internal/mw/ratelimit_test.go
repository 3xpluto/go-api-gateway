package mw

import (
	"net/http"
	"testing"

	"github.com/3xpluto/go-api-gateway/internal/netx"
)

func TestIPResolverTrustedProxyUsesXFF(t *testing.T) {
	set, err := netx.ParseCIDRSet([]string{"10.0.0.0/8"})
	if err != nil {
		t.Fatal(err)
	}
	r := IPResolver{Trusted: set}

	req, _ := http.NewRequest("GET", "http://example.com/", nil)
	req.RemoteAddr = "10.1.2.3:1234" // trusted proxy
	req.Header.Set("X-Forwarded-For", "203.0.113.9, 10.1.2.3")

	if got := r.ClientIP(req); got != "203.0.113.9" {
		t.Fatalf("expected client ip from xff, got %q", got)
	}
}

func TestIPResolverUntrustedIgnoresXFF(t *testing.T) {
	set, err := netx.ParseCIDRSet([]string{"10.0.0.0/8"})
	if err != nil {
		t.Fatal(err)
	}
	r := IPResolver{Trusted: set}

	req, _ := http.NewRequest("GET", "http://example.com/", nil)
	req.RemoteAddr = "192.168.1.5:1234" // not trusted
	req.Header.Set("X-Forwarded-For", "203.0.113.9")

	if got := r.ClientIP(req); got != "192.168.1.5" {
		t.Fatalf("expected remote ip, got %q", got)
	}
}
