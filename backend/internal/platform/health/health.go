package health

import (
	"context"
	"encoding/json"
	"net/http"
)

// Response is the JSON body returned by GET /healthz and GET /readyz.
type Response struct {
	Status string `json:"status"`
}

// CheckFunc reports readiness. A nil error means ready.
type CheckFunc func(ctx context.Context) error

// Handler returns a handler that reports process liveness (no database,
// Valkey, object storage, or vendor dependency).
func Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, Response{Status: "ok"})
	})
}

// ReadyHandler reports process readiness. The response never includes check error details.
// Callers should pass only critical dependencies needed to serve safely (PostgreSQL/PostGIS
// and Valkey when configured). External provider degradation is not process death.
func ReadyHandler(check CheckFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if check == nil || check(r.Context()) != nil {
			writeJSON(w, http.StatusServiceUnavailable, Response{Status: "unavailable"})
			return
		}
		writeJSON(w, http.StatusOK, Response{Status: "ok"})
	})
}

func writeJSON(w http.ResponseWriter, status int, body Response) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
