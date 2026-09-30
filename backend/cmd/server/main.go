// Command server runs the timetable HTTP API.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/istu-pro-dev/timetable/backend/internal/api"
	"github.com/istu-pro-dev/timetable/backend/internal/auth"
	"github.com/istu-pro-dev/timetable/backend/internal/store"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(logger); err != nil {
		logger.Error("server stopped", "err", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	authCfg, warnings, err := auth.ConfigFromEnv(os.Getenv)
	if err != nil {
		return fmt.Errorf("auth config: %w", err)
	}
	for _, w := range warnings {
		logger.Warn(w)
	}

	var st *store.Store
	if dbURL := os.Getenv("DATABASE_URL"); dbURL != "" {
		st, err = store.Open(ctx, dbURL)
		if err != nil {
			return err
		}
		defer st.Close()
		if err := st.Migrate(ctx); err != nil {
			return err
		}
		logger.Info("database migrated")
	} else {
		logger.Warn("DATABASE_URL is not set, running without a database")
	}

	authSvc := auth.NewService(st, authCfg, logger)
	if err := authSvc.Bootstrap(ctx); err != nil {
		return err
	}

	addr := os.Getenv("HTTP_ADDR")
	if addr == "" {
		addr = ":8080"
	}

	srv := &http.Server{
		Addr:              addr,
		Handler:           api.NewRouter(api.Deps{Store: st, Auth: authSvc, Logger: logger}),
		ReadHeaderTimeout: 10 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		logger.Info("listening", "addr", addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
		close(errCh)
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	logger.Info("shutting down")
	return srv.Shutdown(shutdownCtx)
}
