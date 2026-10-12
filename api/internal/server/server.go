// Package server is the token API. It reports the assigned model and the quota.
package server

import (
	"errors"
	"net/http"
	"time"

	"github.com/AlanForester/remote-llm-gateway/internal/httpx"
	"github.com/AlanForester/remote-llm-gateway/internal/metrics"
	"github.com/AlanForester/remote-llm-gateway/internal/store"
)

// Options configures the API server.
type Options struct {
	Store *store.Store
	Now   func() time.Time
}

// New returns the API handler.
func New(opt Options) http.Handler {
	if opt.Now == nil {
		opt.Now = time.Now
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		httpx.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.Handle("GET /metrics", metrics.Handler())
	mux.HandleFunc("GET /v1/me", func(w http.ResponseWriter, r *http.Request) {
		user, err := opt.Store.Authenticate(httpx.Bearer(r), opt.Now())
		if err != nil {
			code := http.StatusInternalServerError
			msg := "internal"
			if errors.Is(err, store.ErrUnauthorized) {
				code = http.StatusUnauthorized
				msg = "unauthorized"
			}
			httpx.WriteJSON(w, code, map[string]string{"error": msg})
			return
		}
		httpx.WriteJSON(w, http.StatusOK, store.Public(user, r.URL.Query().Get("model"), opt.Now()))
	})
	return metrics.Instrument("api", mux)
}
