package ratelimit

import "context"

type Decision struct {
	Allowed           bool
	RetryAfterSeconds int
	Remaining         float64
	LimitRPS          float64
	Burst             float64
}

type Limiter interface {
	Allow(ctx context.Context, key string, rps float64, burst float64, cost float64) (Decision, error)
	Close() error
}
