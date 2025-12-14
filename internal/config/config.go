package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Server    ServerConfig     `yaml:"server"`
	Upstream  UpstreamConfig   `yaml:"upstream"`
	Auth      AuthConfig       `yaml:"auth"`
	RateLimit RateLimitBackend `yaml:"rate_limit"`
	Routes    []RouteConfig    `yaml:"routes"`
}

type ServerConfig struct {
	Addr                     string   `yaml:"addr"`
	TrustedProxies           []string `yaml:"trusted_proxies"`
	MaxHeaderBytes           int      `yaml:"max_header_bytes"`
	MaxBodyBytes             int64    `yaml:"max_body_bytes"`
	ReadTimeoutSeconds       int      `yaml:"read_timeout_seconds"`
	WriteTimeoutSeconds      int      `yaml:"write_timeout_seconds"`
	IdleTimeoutSeconds       int      `yaml:"idle_timeout_seconds"`
	ReadHeaderTimeoutSeconds int      `yaml:"read_header_timeout_seconds"`
}

type UpstreamConfig struct {
	DialTimeoutSeconds           int `yaml:"dial_timeout_seconds"`
	TLSHandshakeTimeoutSeconds   int `yaml:"tls_handshake_timeout_seconds"`
	ResponseHeaderTimeoutSeconds int `yaml:"response_header_timeout_seconds"`
	IdleConnTimeoutSeconds       int `yaml:"idle_conn_timeout_seconds"`
	MaxIdleConns                 int `yaml:"max_idle_conns"`
	MaxIdleConnsPerHost          int `yaml:"max_idle_conns_per_host"`
}

type AuthConfig struct {
	Mode       string         `yaml:"mode"`        // "hmac" | "jwks"
	HMACSecret string         `yaml:"hmac_secret"` // shared secret for HS256 (hmac mode)
	JWKS       JWKSAuthConfig `yaml:"jwks"`        // jwks mode settings
}

type JWKSAuthConfig struct {
	URL                string   `yaml:"url"`
	CacheTTLSeconds    int      `yaml:"cache_ttl_seconds"`
	HTTPTimeoutSeconds int      `yaml:"http_timeout_seconds"`
	LeewaySeconds      int      `yaml:"leeway_seconds"`
	Issuers            []string `yaml:"issuers"`
	Audiences          []string `yaml:"audiences"`
}

type RateLimitBackend struct {
	Backend string         `yaml:"backend"` // "redis" | "memory"
	Redis   RedisConfig    `yaml:"redis"`
	Memory  MemoryRLConfig `yaml:"memory"`
}

type RedisConfig struct {
	Addr     string `yaml:"addr"`
	Password string `yaml:"password"`
	DB       int    `yaml:"db"`
}

type MemoryRLConfig struct {
	CleanupSeconds int `yaml:"cleanup_seconds"`
	TTLSeconds     int `yaml:"ttl_seconds"`
}

type RouteConcurrency struct {
	MaxInFlight int `yaml:"max_in_flight"`
}

type RouteCircuitBreaker struct {
	Enabled             bool `yaml:"enabled"`
	FailureThreshold    int  `yaml:"failure_threshold"`
	OpenSeconds         int  `yaml:"open_seconds"`
	HalfOpenMaxInFlight int  `yaml:"half_open_max_in_flight"`
}

type RouteConfig struct {
	Name           string              `yaml:"name"`
	Match          MatchConfig         `yaml:"match"`
	Upstream       string              `yaml:"upstream"`
	StripPrefix    string              `yaml:"strip_prefix"`
	AuthRequired   bool                `yaml:"auth_required"`
	RateLimit      RouteRLConfig       `yaml:"rate_limit"`
	Concurrency    RouteConcurrency    `yaml:"concurrency"`
	CircuitBreaker RouteCircuitBreaker `yaml:"circuit_breaker"`
}

type MatchConfig struct {
	PathPrefix string `yaml:"path_prefix"`
}

type RouteRLConfig struct {
	Enabled bool    `yaml:"enabled"`
	RPS     float64 `yaml:"rps"`
	Burst   float64 `yaml:"burst"`
	Scope   string  `yaml:"scope"` // "user" | "ip"
}

func Load(path string) (*Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg Config
	if err := yaml.Unmarshal(b, &cfg); err != nil {
		return nil, err
	}

	applyDefaults(&cfg)

	if err := Validate(&cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func applyDefaults(cfg *Config) {
	if cfg.Server.Addr == "" {
		cfg.Server.Addr = ":8080"
	}
	if cfg.Server.MaxHeaderBytes == 0 {
		cfg.Server.MaxHeaderBytes = 1 << 20 // 1 MiB
	}
	if cfg.Server.MaxBodyBytes == 0 {
		cfg.Server.MaxBodyBytes = 1 << 20 // 1 MiB
	}
	if cfg.Server.ReadHeaderTimeoutSeconds == 0 {
		cfg.Server.ReadHeaderTimeoutSeconds = 5
	}
	if cfg.Server.ReadTimeoutSeconds == 0 {
		cfg.Server.ReadTimeoutSeconds = 15
	}
	if cfg.Server.WriteTimeoutSeconds == 0 {
		cfg.Server.WriteTimeoutSeconds = 60
	}
	if cfg.Server.IdleTimeoutSeconds == 0 {
		cfg.Server.IdleTimeoutSeconds = 60
	}

	if cfg.Upstream.DialTimeoutSeconds == 0 {
		cfg.Upstream.DialTimeoutSeconds = 5
	}
	if cfg.Upstream.TLSHandshakeTimeoutSeconds == 0 {
		cfg.Upstream.TLSHandshakeTimeoutSeconds = 5
	}
	if cfg.Upstream.ResponseHeaderTimeoutSeconds == 0 {
		cfg.Upstream.ResponseHeaderTimeoutSeconds = 15
	}
	if cfg.Upstream.IdleConnTimeoutSeconds == 0 {
		cfg.Upstream.IdleConnTimeoutSeconds = 90
	}
	if cfg.Upstream.MaxIdleConns == 0 {
		cfg.Upstream.MaxIdleConns = 100
	}
	if cfg.Upstream.MaxIdleConnsPerHost == 0 {
		cfg.Upstream.MaxIdleConnsPerHost = 20
	}
	// Auth defaults (jwks mode)
	if cfg.Auth.JWKS.CacheTTLSeconds == 0 {
		cfg.Auth.JWKS.CacheTTLSeconds = 300
	}
	if cfg.Auth.JWKS.HTTPTimeoutSeconds == 0 {
		cfg.Auth.JWKS.HTTPTimeoutSeconds = 3
	}
	if cfg.Auth.JWKS.LeewaySeconds == 0 {
		cfg.Auth.JWKS.LeewaySeconds = 30
	}

}

func Validate(cfg *Config) error {
	if len(cfg.Routes) == 0 {
		return errors.New("no routes configured")
	}

	seenNames := map[string]struct{}{}
	for i, r := range cfg.Routes {
		idx := fmt.Sprintf("routes[%d]", i)
		name := strings.TrimSpace(r.Name)
		if name == "" {
			return fmt.Errorf("%s.name is required", idx)
		}
		if _, ok := seenNames[name]; ok {
			return fmt.Errorf("duplicate route name: %q", name)
		}
		seenNames[name] = struct{}{}

		pp := strings.TrimSpace(r.Match.PathPrefix)
		if pp == "" || !strings.HasPrefix(pp, "/") {
			return fmt.Errorf("%s.match.path_prefix must start with '/'", idx)
		}

		if r.Upstream == "" {
			return fmt.Errorf("%s.upstream is required", idx)
		}
		if _, err := url.Parse(r.Upstream); err != nil {
			return fmt.Errorf("%s.upstream invalid: %v", idx, err)
		}

		if r.StripPrefix != "" && !strings.HasPrefix(r.StripPrefix, "/") {
			return fmt.Errorf("%s.strip_prefix must start with '/' if set", idx)
		}

		if r.RateLimit.Enabled {
			if r.RateLimit.RPS <= 0 {
				return fmt.Errorf("%s.rate_limit.rps must be > 0 when enabled", idx)
			}
			if r.RateLimit.Burst <= 0 {
				return fmt.Errorf("%s.rate_limit.burst must be > 0 when enabled", idx)
			}
			s := strings.ToLower(strings.TrimSpace(r.RateLimit.Scope))
			if s != "ip" && s != "user" {
				return fmt.Errorf("%s.rate_limit.scope must be 'ip' or 'user'", idx)
			}
		}
	}

	backend := strings.ToLower(strings.TrimSpace(cfg.RateLimit.Backend))
	if backend != "redis" && backend != "memory" {
		return fmt.Errorf("rate_limit.backend must be 'redis' or 'memory'")
	}
	if backend == "redis" && strings.TrimSpace(cfg.RateLimit.Redis.Addr) == "" {
		return fmt.Errorf("rate_limit.redis.addr is required when backend is redis")
	}
	if cfg.Auth.Mode != "" {
		mode := strings.ToLower(strings.TrimSpace(cfg.Auth.Mode))
		switch mode {
		case "hmac":
			if strings.TrimSpace(cfg.Auth.HMACSecret) == "" {
				return fmt.Errorf("auth.hmac_secret is required when auth.mode is hmac")
			}
		case "jwks":
			if strings.TrimSpace(cfg.Auth.JWKS.URL) == "" {
				return fmt.Errorf("auth.jwks.url is required when auth.mode is jwks")
			}
			if _, err := url.Parse(cfg.Auth.JWKS.URL); err != nil {
				return fmt.Errorf("auth.jwks.url invalid: %v", err)
			}
		default:
			return fmt.Errorf("auth.mode must be 'hmac' or 'jwks'")
		}
	}
	return nil
}
