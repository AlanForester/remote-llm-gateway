package metrics

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestInstrumentRecordsStatus(t *testing.T) {
	before := counterValue(t)
	h := Instrument("test", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	h.ServeHTTP(httptest.NewRecorder(), req)
	if got := counterValue(t); got <= before {
		t.Fatalf("requests = %v, want an increase from %v", got, before)
	}

	req = httptest.NewRequest(http.MethodGet, "/metrics", nil)
	h.ServeHTTP(httptest.NewRecorder(), req)
	body := httptest.NewRecorder()
	Handler().ServeHTTP(body, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if !strings.Contains(body.Body.String(), "remote_llm_http_requests_total") {
		t.Fatalf("metrics body missing counter:\n%s", body.Body.String())
	}
}

func counterValue(t *testing.T) float64 {
	t.Helper()
	c, err := Requests.GetMetricWithLabelValues("test", "204")
	if err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	// Read through the public gatherer so the series exists after the first call.
	rec := httptest.NewRecorder()
	Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	out.WriteString(rec.Body.String())
	n := 0.0
	for _, line := range strings.Split(out.String(), "\n") {
		if strings.HasPrefix(line, `remote_llm_http_requests_total{`) && strings.Contains(line, `service="test"`) && strings.Contains(line, `code="204"`) {
			fields := strings.Fields(line)
			if len(fields) == 2 {
				return parseFloat(t, fields[1])
			}
		}
	}
	_ = c
	return n
}

func parseFloat(t *testing.T, s string) float64 {
	t.Helper()
	var n float64
	sign := 1.0
	for _, r := range s {
		if r == '-' {
			sign = -1
			continue
		}
		if r < '0' || r > '9' {
			t.Fatalf("not an integer metric %q", s)
		}
		n = n*10 + float64(r-'0')
	}
	return n * sign
}
