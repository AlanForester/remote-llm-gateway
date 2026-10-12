// Package httpx holds small helpers shared by the HTTP servers.
package httpx

import (
	"encoding/json"
	"net/http"
	"strings"
)

// WriteJSON writes a JSON response.
func WriteJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

// ReadJSON decodes a request body.
func ReadJSON(r *http.Request, v any) error {
	defer r.Body.Close()
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	return dec.Decode(v)
}

// Bearer reads an Authorization header or a token query parameter.
func Bearer(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if strings.HasPrefix(strings.ToLower(h), "bearer ") {
		return strings.TrimSpace(h[len("bearer "):])
	}
	return strings.TrimSpace(r.URL.Query().Get("token"))
}
