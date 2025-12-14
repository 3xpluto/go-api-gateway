package ratelimit

import (
	"context"
	"time"

	"github.com/redis/go-redis/v9"
)

const tokenBucketLua = `
local key = KEYS[1]
local now_ms = tonumber(ARGV[1])
local rate = tonumber(ARGV[2])
local burst = tonumber(ARGV[3])
local cost = tonumber(ARGV[4])

local data = redis.call("HMGET", key, "tokens", "ts")
local tokens = tonumber(data[1])
local ts = tonumber(data[2])

if tokens == nil then
  tokens = burst
  ts = now_ms
else
  local delta = math.max(0, now_ms - ts)
  local add = (delta / 1000.0) * rate
  tokens = math.min(burst, tokens + add)
  ts = now_ms
end

local allowed = 0
local retry_ms = 0

if tokens >= cost then
  allowed = 1
  tokens = tokens - cost
else
  allowed = 0
  local missing = cost - tokens
  if rate > 0 then
    retry_ms = math.floor((missing / rate) * 1000.0)
  else
    retry_ms = 1000
  end
end

redis.call("HMSET", key, "tokens", tokens, "ts", ts)
redis.call("PEXPIRE", key, 300000)
return {allowed, tokens, retry_ms}
`

type RedisLimiter struct {
	rdb *redis.Client
}

func NewRedisLimiter(rdb *redis.Client) *RedisLimiter {
	return &RedisLimiter{rdb: rdb}
}

func (r *RedisLimiter) Allow(ctx context.Context, key string, rps float64, burst float64, cost float64) (Decision, error) {
	now := time.Now().UnixMilli()
	res, err := r.rdb.Eval(ctx, tokenBucketLua, []string{key}, now, rps, burst, cost).Result()
	if err != nil {
		return Decision{}, err
	}
	arr, ok := res.([]any)
	if !ok || len(arr) != 3 {
		return Decision{}, redis.Nil
	}
	allowed := toInt(arr[0]) == 1
	tokens := toFloat(arr[1])
	retryMs := toInt(arr[2])

	dec := Decision{Allowed: allowed, Remaining: tokens, LimitRPS: rps, Burst: burst}
	if !allowed {
		dec.RetryAfterSeconds = int((retryMs + 999) / 1000)
	}
	return dec, nil
}

func (r *RedisLimiter) Close() error { return r.rdb.Close() }

func toInt(v any) int64 {
	switch t := v.(type) {
	case int64:
		return t
	case int:
		return int64(t)
	case float64:
		return int64(t)
	default:
		return 0
	}
}

func toFloat(v any) float64 {
	switch t := v.(type) {
	case float64:
		return t
	case int64:
		return float64(t)
	case int:
		return float64(t)
	default:
		return 0
	}
}
