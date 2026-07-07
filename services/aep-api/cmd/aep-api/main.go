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

package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/wso2/aep/aep-api/internal/app"
	"github.com/wso2/aep/aep-api/internal/config"
	"github.com/wso2/aep/aep-api/internal/database"
	"github.com/wso2/aep/aep-api/internal/platform/obs"
	"github.com/wso2/aep/aep-api/models"
)

// main is the process entry point and owns only process lifecycle: load+validate
// config, open the DB, run first-boot schema steps (app.Bootstrap), assemble the
// service graph (app.Build), then serve until a signal. All wiring lives in
// internal/app so it is reachable from a test with faked deps; main holds no
// business logic.
func main() {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to load config: %v\n", err)
		os.Exit(1)
	}

	setupLogger(cfg.LogLevel)

	// Database. Executions + ComponentConfig + webhook tables. Tasks are GitHub
	// issues now (no component_tasks table — dropped by the tasks-github-native
	// migration). org_credentials lives in git-service — the BFF does not
	// auto-migrate or read it locally.
	db, err := database.Open(cfg.DatabaseURL,
		&models.ComponentConfig{},
		&models.WebhookDelivery{},
		&models.WebhookPayload{},
		&models.Organization{},
		&models.Execution{},
		&models.AgentTurn{},
		&models.DevelopmentCycle{},
	)
	if err != nil {
		slog.Error("database init failed", "error", err)
		os.Exit(1)
	}

	// Schema bootstrap + migrations (grants + RunAll). Kept out of Build so the
	// composition root stays a pure assembly.
	if err := app.Bootstrap(context.Background(), db, cfg); err != nil {
		slog.Error("bootstrap failed", "error", err)
		os.Exit(1)
	}

	application, err := app.Build(cfg, db)
	if err != nil {
		slog.Error("app init failed", "error", err)
		os.Exit(1)
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
	// the next tick — a plain goroutine per watcher is enough. All share
	// watcherCtx, cancelled on shutdown.
	watcherCtx, cancelWatcher := context.WithCancel(context.Background())
	defer cancelWatcher()
	for _, w := range application.Watchers {
		go w.Run(watcherCtx)
	}
	slog.Info("background watchers started", "count", len(application.Watchers))

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	slog.Info("shutting down server")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		slog.Error("server shutdown failed", "error", err)
		os.Exit(1)
	}
	slog.Info("server stopped")
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
