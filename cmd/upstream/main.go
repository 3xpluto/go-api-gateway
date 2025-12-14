package main

import (
	"encoding/json"
	"flag"
	"net/http"
	"time"
)

func main() {
	var addr string
	var name string
	var delayMS int
	flag.StringVar(&addr, "addr", ":9001", "listen address")
	flag.StringVar(&name, "name", "upstream", "service name")
	flag.IntVar(&delayMS, "delay-ms", 0, "artificial delay per request")
	flag.Parse()

	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if delayMS > 0 {
			time.Sleep(time.Duration(delayMS) * time.Millisecond)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"service": name,
			"method":  r.Method,
			"path":    r.URL.Path,
			"query":   r.URL.RawQuery,
			"headers": r.Header,
		})
	})

	srv := &http.Server{Addr: addr, Handler: h}
	_ = srv.ListenAndServe()
}
