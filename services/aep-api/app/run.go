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

package app

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	intapp "github.com/wso2/aep/aep-api/internal/app"
	"github.com/wso2/aep/aep-api/internal/clients/secretmanagersvc"
	"github.com/wso2/aep/aep-api/internal/config"
	"github.com/wso2/aep/aep-api/secretsprovider"
	"github.com/wso2/aep/aep-api/internal/platform/async"
	"github.com/wso2/aep/aep-api/internal/platform/obs"
)

// Run owns config load → resolve → assemble → HTTP serve → watchers → signal
// shutdown. opts nil-fields are feature off-switches (see Options); they never
// panic and never silently pick a different credential path.
func Run(opts Options) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	setupLogger(cfg.LogLevel)

	infra, err := intapp.Resolve(context.Background(), cfg)
	if err != nil {
		return fmt.Errorf("infra resolve failed: %w", err)
	}

	resolver := opts.ImpersonateOrgResolver
	if resolver == nil && opts.ImpersonateOrgResolverBuilder != nil {
		resolver = opts.ImpersonateOrgResolverBuilder(infra.DB)
	}

	secretsProvider := adaptSecretsProvider(opts.SecretsProvider)

	application, err := intapp.Assemble(cfg, infra, intapp.Seam{
		AuthProvider:           opts.AuthProvider,
		RequestAuthStrategy:    opts.RequestAuthStrategy,
		ImpersonateOrgResolver: resolver,
		SecretsProvider:        secretsProvider,
	})
	if err != nil {
		return fmt.Errorf("app init failed: %w", err)
	}
	for _, deg := range application.Degradations() {
		slog.Warn("capability degraded", "capability", deg.Capability, "reason", deg.Reason)
	}

	server := &http.Server{
		Addr:              fmt.Sprintf("%s:%d", cfg.ServerHost, cfg.ServerPort),
		Handler:           application.Handler,
		ReadHeaderTimeout: 15 * time.Second,
		WriteTimeout:      15 * time.Minute, // AI design generation can take up to 10 min
		IdleTimeout:       60 * time.Second,
	}

	go func() {
		slog.Info("server started", "addr", server.Addr)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("server error", "error", err)
			os.Exit(1)
		}
	}()

	// Background watchers. State lives in Postgres, so a restart resumes from
	// the next tick. Each runs under async.Go's panic barrier so a panicking
	// watcher is recovered + logged instead of taking down the whole process
	// (a bare `go w.Run` used to). All share watcherCtx, cancelled on shutdown.
	watcherCtx, cancelWatcher := context.WithCancel(context.Background())
	defer cancelWatcher()
	for _, w := range application.Watchers {
		async.Go(watcherCtx, fmt.Sprintf("watcher:%T", w), w.Run)
	}
	slog.Info("background watchers started", "count", len(application.Watchers))

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	slog.Info("shutting down server")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		return fmt.Errorf("server shutdown failed: %w", err)
	}
	slog.Info("server stopped")
	return nil
}

func adaptSecretsProvider(v secretsprovider.Provider) secretmanagersvc.Provider {
	return v
}

func setupLogger(level string) {
	var logLevel slog.Level
	switch level {
	case "debug":
		logLevel = slog.LevelDebug
	case "warn":
		logLevel = slog.LevelWarn
	case "error":
		logLevel = slog.LevelError
	default:
		logLevel = slog.LevelInfo
	}
	base := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: logLevel})
	slog.SetDefault(slog.New(obs.NewContextHandler(base)))
}
