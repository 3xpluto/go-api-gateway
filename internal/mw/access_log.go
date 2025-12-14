package mw

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/yourname/apigw/internal/httpx"
)

func AccessLog(log *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sw := &httpx.StatusWriter{ResponseWriter: w}
		start := time.Now()
		next.ServeHTTP(sw, r)
		d := time.Since(start)

	log.Info("http_request",
		slog.String("rid", RID(r.Context())),
		slog.String("route", RouteName(r.Context())),
		slog.String("method", r.Method),
		slog.String("path", r.URL.Path),
		slog.String("remote", r.RemoteAddr),
		slog.Int("status", sw.Status),
		slog.Int("bytes", sw.Bytes),
		slog.String("duration", d.String()),
	)
	})
}
