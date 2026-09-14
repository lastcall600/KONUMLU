package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"backend/internal/platform/health"
)

func TestTRGatewayHealthIndependentOfGermany(t *testing.T) {
	mux := http.NewServeMux()
	mux.Handle("GET /healthz", health.Handler())
	mux.Handle("GET /readyz", health.ReadyHandler(func(context.Context) error { return nil }))
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("healthz=%d", rec.Code)
	}
	req = httptest.NewRequest(http.MethodGet, "/readyz", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("readyz=%d", rec.Code)
	}
}
