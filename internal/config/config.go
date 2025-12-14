package config

import (
	"errors"
	"os"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Server    ServerConfig     `yaml:"server"`
	Auth      AuthConfig       `yaml:"auth"`
	RateLimit RateLimitBackend `yaml:"rate_limit"`
	Routes    []RouteConfig    `yaml:"routes"`
}

type ServerConfig struct {
	Addr string `yaml:"addr"`
}

type AuthConfig struct {
	Mode       string `yaml:"mode"`        // "hmac"
	HMACSecret string `yaml:"hmac_secret"` // shared secret for HS256
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

type RouteConfig struct {
	Name         string        `yaml:"name"`
	Match        MatchConfig   `yaml:"match"`
	Upstream     string        `yaml:"upstream"`
	StripPrefix  string        `yaml:"strip_prefix"`
	AuthRequired bool          `yaml:"auth_required"`
	RateLimit    RouteRLConfig `yaml:"rate_limit"`
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
	if cfg.Server.Addr == "" {
		cfg.Server.Addr = ":8080"
	}
	if len(cfg.Routes) == 0 {
		return nil, errors.New("no routes configured")
	}
	return &cfg, nil
}
