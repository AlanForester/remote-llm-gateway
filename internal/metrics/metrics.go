// Package metrics exposes Prometheus series for the three processes.
package metrics

import (
	"bufio"
	"errors"
	"net"
	"net/http"
	"strconv"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var errNotHijacker = errors.New("response does not support hijacking")

var (
	// Requests counts HTTP responses, except scrapes of /metrics.
	Requests = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "remote_llm_http_requests_total",
		Help: "HTTP responses served by this process.",
	}, []string{"service", "code"})

	// Tokens counts tokens recorded by the gateway.
	Tokens = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "remote_llm_tokens_total",
		Help: "Tokens recorded after an agent reply.",
	}, []string{"model"})

	// QuotaDenied counts requests refused because the period limit is spent.
	QuotaDenied = promauto.NewCounter(prometheus.CounterOpts{
		Name: "remote_llm_quota_denied_total",
		Help: "Requests refused because the token limit for the period is spent.",
	})

	// Sessions is how many session directories the process still holds.
	Sessions = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "remote_llm_sessions",
		Help: "Session directories held in memory. They do not survive a restart.",
	})
)

// Handler serves the Prometheus text format.
func Handler() http.Handler { return promhttp.Handler() }

// Instrument records the status code of next. WebSocket upgrades pass through.
func Instrument(service string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sw := &statusWriter{ResponseWriter: w, code: http.StatusOK}
		next.ServeHTTP(sw, r)
		if r.URL.Path == "/metrics" {
			return
		}
		Requests.WithLabelValues(service, strconv.Itoa(sw.code)).Inc()
	})
}

type statusWriter struct {
	http.ResponseWriter
	code  int
	wrote bool
}

func (s *statusWriter) WriteHeader(code int) {
	if !s.wrote {
		s.code = code
		s.wrote = true
	}
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusWriter) Write(b []byte) (int, error) {
	if !s.wrote {
		s.code = http.StatusOK
		s.wrote = true
	}
	return s.ResponseWriter.Write(b)
}

func (s *statusWriter) Unwrap() http.ResponseWriter { return s.ResponseWriter }

func (s *statusWriter) Flush() {
	if f, ok := s.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (s *statusWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	h, ok := s.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, errNotHijacker
	}
	return h.Hijack()
}
