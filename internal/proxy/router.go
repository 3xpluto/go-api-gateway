package proxy

import (
	"encoding/json"
	"net/http"
	"net/http/httputil"
	"net/url"
	"sort"
	"strings"
)

type Route struct {
	Name         string
	PathPrefix   string
	Upstream     *url.URL
	StripPrefix  string
	AuthRequired bool
	RateLimit    RouteRateLimit
	Proxy        *httputil.ReverseProxy
}

type RouteRateLimit struct {
	Enabled bool
	RPS     float64
	Burst   float64
	Scope   string
}

type Router struct {
	routes []Route
}

func New(routes []Route) (*Router, error) {
	if len(routes) == 0 {
		return nil, ErrNoRoutes
	}
	sort.Slice(routes, func(i, j int) bool {
		return len(routes[i].PathPrefix) > len(routes[j].PathPrefix)
	})
	return &Router{routes: routes}, nil
}

var ErrNoRoutes = &errString{s: "no routes"}

type errString struct{ s string }

func (e *errString) Error() string { return e.s }

func (r *Router) Match(path string) *Route {
	for i := range r.routes {
		if strings.HasPrefix(path, r.routes[i].PathPrefix) {
			return &r.routes[i]
		}
	}
	return nil
}

func BuildProxy(up *url.URL, transport http.RoundTripper) *httputil.ReverseProxy {
	p := httputil.NewSingleHostReverseProxy(up)
	p.Transport = transport

	orig := p.Director
	p.Director = func(req *http.Request) {
		orig(req)
		req.Host = up.Host
	}

	p.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		msg := ""
		code := http.StatusBadGateway
		if err != nil {
			msg = err.Error()
			if strings.Contains(msg, "request body too large") {
				code = http.StatusRequestEntityTooLarge
				msg = "request_too_large"
			}
		}
		w.WriteHeader(code)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error": msg,
		})
	}

	return p
}

func StripPath(path string, strip string) string {
	if strip == "" {
		return path
	}
	if strings.HasPrefix(path, strip) {
		p := strings.TrimPrefix(path, strip)
		if p == "" {
			p = "/"
		}
		return p
	}
	return path
}
