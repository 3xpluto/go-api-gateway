package mw

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/yourname/apigw/internal/httpx"
)

type Metrics struct {
	Requests *prometheus.CounterVec
	Latency  *prometheus.HistogramVec
}

func NewMetrics(reg prometheus.Registerer) *Metrics {
	m := &Metrics{
		Requests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "apigw_http_requests_total",
			Help: "Total HTTP requests processed by the gateway",
		}, []string{"route", "method", "code"}),
		Latency: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "apigw_http_request_duration_seconds",
			Help:    "HTTP request latency",
			Buckets: prometheus.DefBuckets,
		}, []string{"route", "method"}),
	}
	reg.MustRegister(m.Requests, m.Latency)
	return m
}

type routeKeyType string

const routeKey routeKeyType = "route"

func WithRoute(next http.Handler, routeName string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r = r.WithContext(context.WithValue(r.Context(), routeKey, routeName))
		next.ServeHTTP(w, r)
	})
}

func RouteName(ctx context.Context) string {
	if v, ok := ctx.Value(routeKey).(string); ok && v != "" {
		return v
	}
	return "unknown"
}

func Instrument(m *Metrics, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sw := &httpx.StatusWriter{ResponseWriter: w}
		start := time.Now()
		next.ServeHTTP(sw, r)
		route := RouteName(r.Context())
		code := sw.Status
		if code == 0 {
			code = http.StatusOK
		}
		m.Requests.WithLabelValues(route, r.Method, strconv.Itoa(code)).Inc()
		m.Latency.WithLabelValues(route, r.Method).Observe(time.Since(start).Seconds())
	})
}
