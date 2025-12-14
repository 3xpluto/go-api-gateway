package proxy

import (
	"errors"
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
		return nil, errors.New("no routes")
	}
	// longest prefix match: sort desc by prefix length
	sort.Slice(routes, func(i, j int) bool {
		return len(routes[i].PathPrefix) > len(routes[j].PathPrefix)
	})
	return &Router{routes: routes}, nil
}

func (r *Router) Match(path string) *Route {
	for i := range r.routes {
		if strings.HasPrefix(path, r.routes[i].PathPrefix) {
			return &r.routes[i]
		}
	}
	return nil
}

func BuildProxy(up *url.URL) *httputil.ReverseProxy {
	p := httputil.NewSingleHostReverseProxy(up)
	orig := p.Director
	p.Director = func(req *http.Request) {
		orig(req)
		// keep original Host? for upstream routing you may want req.Host = up.Host
		req.Host = up.Host
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
