package main

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"backend/internal/eids/trdecision"
	eidsadapter "backend/internal/infrastructure/eids"
	"backend/internal/platform/config"
	"backend/internal/platform/health"
	"backend/internal/platform/observability"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	cfg, err := config.LoadTRSigner()
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}
	observability.ConfigureJSON(config.Config{Environment: config.EnvDevelopment, LogLevel: "info", HTTPAddr: ":8081"}, nil)
	if cfg.Enabled {
		if _, err := trdecision.ParsePrivateKey(cfg.KeyID, cfg.PrivateKeyRaw); err != nil {
			return fmt.Errorf("signing key: %w", err)
		}
		if _, err := eidsadapter.NewDeliveryClient(cfg.GermanyURL, cfg.IngressToken, cfg.DeliveryTimeout); err != nil {
			return fmt.Errorf("delivery: %w", err)
		}
		slog.Info("tr_gateway_signer_ready", "key_id", cfg.KeyID)
	} else {
		slog.Info("tr_gateway_signer_disabled")
	}

	mux := http.NewServeMux()
	mux.Handle("GET /healthz", health.Handler())
	mux.Handle("GET /readyz", health.ReadyHandler(func(context.Context) error { return nil }))

	addr := stringsOr(":8081", os.Getenv("HTTP_ADDR"))
	srv := &http.Server{Addr: addr, Handler: observability.Wrap(mux)}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	errCh := make(chan error, 1)
	go func() {
		slog.Info("listening", "addr", addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
	}()
	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
	}
	shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return srv.Shutdown(shutdown)
}

func stringsOr(def, raw string) string {
	if raw == "" {
		return def
	}
	return raw
}
