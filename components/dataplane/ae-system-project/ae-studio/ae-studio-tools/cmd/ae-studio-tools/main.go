// Copyright (c) 2026, WSO2 LLC. (https://www.wso2.com).
//
// WSO2 LLC. licenses this file to you under the Apache License,
// Version 2.0 (the "License"); you may not use this file except
// in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing,
// software distributed under the License is distributed on an
// "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
// KIND, either express or implied.  See the License for the
// specific language governing permissions and limitations
// under the License.

// Command ae-studio-tools is the AE Studio pod's tools container. This is
// wiring only; the public listener, auth and routes are added by later tasks.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/wso2/aep/ae-studio-tools/internal/config"
	"github.com/wso2/aep/ae-studio-tools/internal/edge"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
	if err := run(); err != nil {
		os.Exit(1)
	}
}

func run() error {
	if err := config.CheckSecretRev(os.Getenv); err != nil {
		slog.Error("secret_rev_mismatch", "error", err)
		return err
	}
	cfg, err := config.Load(os.Getenv)
	if err != nil {
		slog.Error("config_invalid", "error", err)
		return err
	}

	var ready atomic.Bool
	health := &http.Server{
		Addr:              fmt.Sprintf(":%d", cfg.HealthPort),
		Handler:           edge.NewHealth(ready.Load),
		ReadHeaderTimeout: 5 * time.Second,
	}
	serveErr := make(chan error, 1)
	go func() { serveErr <- health.ListenAndServe() }()
	slog.Info("health_listening", "port", cfg.HealthPort)

	// The public listener is added later; ready flips once it is bound.
	ready.Store(true)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, os.Interrupt)
	defer stop()
	select {
	case err := <-serveErr:
		slog.Error("health_listener_failed", "error", err)
		return err
	case <-ctx.Done():
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := health.Shutdown(shutdownCtx); err != nil && !errors.Is(err, http.ErrServerClosed) {
		slog.Error("shutdown_failed", "error", err)
		return err
	}
	slog.Info("stopped")
	return nil
}
