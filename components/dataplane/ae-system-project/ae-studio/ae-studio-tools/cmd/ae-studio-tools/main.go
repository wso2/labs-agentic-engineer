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

// Command ae-studio-tools is the AE Studio pod's tools container: wiring only.
// It builds the git engine over the studio-data volume and its reaper, the
// per-request project resolver (aep-api, with the org's publisher token), and
// serves the public listener (edge.Routes) and the health listener, reporting
// ready once both are bound. SIGTERM (tini forwards it) shuts the listeners
// down, then stops the reaper.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/wso2/aep/ae-studio-tools/internal/auth"
	"github.com/wso2/aep/ae-studio-tools/internal/config"
	"github.com/wso2/aep/ae-studio-tools/internal/edge"
	"github.com/wso2/aep/ae-studio-tools/internal/files"
	"github.com/wso2/aep/ae-studio-tools/internal/github"
	"github.com/wso2/aep/ae-studio-tools/internal/platform"
	"github.com/wso2/aep/ae-studio-tools/internal/projects"
	"github.com/wso2/aep/ae-studio-tools/internal/repo"
	"github.com/wso2/aep/ae-studio-tools/internal/repo/reaper"
	"github.com/wso2/aep/ae-studio-tools/internal/webhook"
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

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, os.Interrupt)
	defer stop()

	// The git engine and its reaper. reaper.New registers the engine's ENOSPC
	// hook, so it runs before any listener can serve a read.
	engine, layout, err := repo.New(cfg.StudioDataDir, repo.StaticToken(cfg.GitHubPAT))
	if err != nil {
		slog.Error("repo_init_failed", "error", err)
		return err
	}
	slog.Info("repo.root", "root_layout", string(layout))
	reap := reaper.New(engine, reaper.Config{Budget: cfg.StorageBudgetBytes})

	// The project resolver: aep-api on every request, as the org's publisher.
	aepAPI, err := platform.NewAEPAPI(cfg.AEPAPIBaseURL, &platform.ClientCredentials{
		TokenURL: cfg.IDPTokenURL, ClientID: cfg.PublisherClientID, ClientSecret: cfg.PublisherClientSecret,
	})
	if err != nil {
		slog.Error("aep_api_client_invalid")
		return err
	}
	reader := files.Reader{Engine: engine, Projects: projects.NewAEPAPIResolver(aepAPI), Org: cfg.OrgHandle}

	reaperCtx, stopReaper := context.WithCancel(context.Background())
	defer stopReaper()
	reaperDone := make(chan struct{})
	go func() {
		defer close(reaperDone)
		reap.Run(reaperCtx)
	}()

	var ready atomic.Bool
	public := &http.Server{
		Addr: fmt.Sprintf(":%d", cfg.ListenPort),
		Handler: edge.Routes(edge.Deps{
			Cfg:      cfg,
			Verifier: auth.NewVerifier(cfg.IDPIssuer, auth.NewJWKSCache(cfg.IDPJWKSURL)),
			GitHub:   github.NewClient(cfg.GitHubPAT),
			Webhook:  edge.WebhookHandler(cfg.WebhookSecret, webhook.Unwired()),
			Files:    reader,
		}),
		ReadHeaderTimeout: 10 * time.Second,
	}
	health := &http.Server{
		Addr:              fmt.Sprintf(":%d", cfg.HealthPort),
		Handler:           edge.NewHealth(ready.Load),
		ReadHeaderTimeout: 5 * time.Second,
	}

	healthLn, err := net.Listen("tcp", health.Addr)
	if err != nil {
		slog.Error("health_listen_failed", "port", cfg.HealthPort, "error", err)
		stopReaper()
		<-reaperDone
		return err
	}
	slog.Info("health_listening", "port", cfg.HealthPort)
	publicLn, err := net.Listen("tcp", public.Addr)
	if err != nil {
		_ = healthLn.Close()
		slog.Error("public_listen_failed", "port", cfg.ListenPort, "error", err)
		stopReaper()
		<-reaperDone
		return err
	}
	slog.Info("public_listening", "port", cfg.ListenPort)

	serveErr := make(chan error, 2)
	go func() { serveErr <- fmt.Errorf("health listener: %w", health.Serve(healthLn)) }()
	go func() { serveErr <- fmt.Errorf("public listener: %w", public.Serve(publicLn)) }()
	// Both sockets are bound, so a probe that sees ready can reach the routes.
	ready.Store(true)

	var runErr error
	select {
	case runErr = <-serveErr:
		slog.Error("listener_failed", "error", runErr)
	case <-ctx.Done():
	}
	ready.Store(false)

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for _, srv := range []*http.Server{public, health} {
		if err := srv.Shutdown(shutdownCtx); err != nil {
			slog.Error("shutdown_failed", "addr", srv.Addr, "error", err)
			runErr = err
		}
	}
	// The listeners are drained, so no request can still be reading; the
	// reaper's git children die with its context.
	stopReaper()
	select {
	case <-reaperDone:
	case <-shutdownCtx.Done():
		slog.Error("reaper_stop_timeout")
	}
	if runErr != nil {
		return runErr
	}
	slog.Info("stopped")
	return nil
}
