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
// serves the public listener (edge.Routes), the Files socket
// (edge.FilesSocketRoutes, ae-collab's), the MCP socket
// (edge.MCPSocketRoutes, ae-design-agent's) and the health listener,
// reporting ready once the public listener and both sockets are bound.
// SIGTERM (tini forwards it) drains the public and health listeners, keeps
// both sockets serving through the drain window for ae-collab's final flush
// and the agent's outbox drain (07 §10), shuts them down, then stops the
// reaper.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/wso2/aep/ae-studio-tools/internal/auth"
	"github.com/wso2/aep/ae-studio-tools/internal/config"
	"github.com/wso2/aep/ae-studio-tools/internal/edge"
	"github.com/wso2/aep/ae-studio-tools/internal/files"
	"github.com/wso2/aep/ae-studio-tools/internal/github"
	"github.com/wso2/aep/ae-studio-tools/internal/mcp"
	"github.com/wso2/aep/ae-studio-tools/internal/platform"
	"github.com/wso2/aep/ae-studio-tools/internal/projects"
	"github.com/wso2/aep/ae-studio-tools/internal/repo"
	"github.com/wso2/aep/ae-studio-tools/internal/repo/reaper"
	"github.com/wso2/aep/ae-studio-tools/internal/webhook"
)

// reaperStopTimeout bounds the wait for the reaper after its context is
// canceled (its git children are killed with it), separate from the listener
// drain's budget.
const reaperStopTimeout = 2 * time.Second

// Shutdown budget, inside the pod's 30 s termination grace (Q-5/D-5):
//
//	socketDrainWindow (10 s) + socketShutdownTimeout (15 s) + reaperStopTimeout (2 s) = 27 s < 30 s
//
// The public and health listeners drain (listenerDrainTimeout, 5 s) inside the
// window. Both sockets keep accepting through the one window and are then
// shut down concurrently, so the second socket adds nothing to the total; a
// step that must run beside them (Task 3.7's usage flush, at most 3 s) joins
// that same concurrent group, inside socketShutdownTimeout.
const (
	// listenerDrainTimeout bounds the public and health listeners' drain.
	listenerDrainTimeout = 5 * time.Second
	// socketDrainWindow is how long after SIGTERM the Files and MCP sockets
	// keep accepting: ae-collab and ae-design-agent get SIGTERM at the same
	// moment, ae-collab flushes every Room through the Files socket and the
	// agent drains its outbox through the MCP socket (07 §10). Coupled to
	// ae-collab's shutdown flush budget (SHUTDOWN_FLUSH_BUDGET_MS, 8 s,
	// ae-collab/src/committer.ts) and the agent's outbox drain (8 s), which
	// must end inside this window with a margin; raise them together.
	socketDrainWindow = 10 * time.Second
	// socketShutdownTimeout bounds the wait for requests still in flight on
	// either socket once they stop accepting.
	socketShutdownTimeout = 15 * time.Second
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
	// The MCP socket: remote-git in the pod with the gitpat for the org's own
	// GitHub account, the other tools forwarded to aep-api as the publisher,
	// the agent's room token minted as ae-studio-<org>, and the project and
	// skills snapshots the agent reads.
	mcpDeps := edge.MCPSocketDeps{
		MCP: mcp.Server{
			Remote:   mcp.RemoteGit{Owner: cfg.GitHubOwner, Token: cfg.GitHubPAT},
			Upstream: mcp.NewAEPAPIUpstream(aepAPI),
		},
		RoomTokens: &platform.ClientCredentials{
			TokenURL: cfg.IDPTokenURL, ClientID: cfg.StudioClientID, ClientSecret: cfg.StudioClientSecret,
		},
		Snapshots: reader,
	}
	gh := github.NewClient(cfg.GitHubPAT)
	applier := files.Applier{
		Reader:    reader,
		Completer: files.NewAEPAPICompleter(aepAPI),
		Identity:  github.NewCommitAuthor(gh),
	}

	reaperCtx, stopReaper := context.WithCancel(context.Background())
	defer stopReaper()
	reaperDone := make(chan struct{})
	go func() {
		defer close(reaperDone)
		reap.Run(reaperCtx)
	}()

	var ready edge.Readiness
	public := &http.Server{
		Addr: fmt.Sprintf(":%d", cfg.ListenPort),
		Handler: edge.Routes(edge.Deps{
			Cfg:      cfg,
			Verifier: auth.NewVerifier(cfg.IDPIssuer, auth.NewJWKSCache(cfg.IDPJWKSURL)),
			GitHub:   gh,
			Webhook:  edge.WebhookHandler(cfg.WebhookSecret, webhook.Unwired()),
			Files:    reader,
		}),
		ReadHeaderTimeout: 10 * time.Second,
	}
	health := &http.Server{
		Addr:              fmt.Sprintf(":%d", cfg.HealthPort),
		Handler:           edge.NewHealth(ready.Ready),
		ReadHeaderTimeout: 5 * time.Second,
	}
	filesSock := &http.Server{
		Addr:              cfg.FilesSocket,
		Handler:           edge.FilesSocketRoutes(applier),
		ReadHeaderTimeout: 10 * time.Second,
	}
	mcpSock := &http.Server{
		Addr:              cfg.MCPSocket,
		Handler:           edge.MCPSocketRoutes(mcpDeps),
		ReadHeaderTimeout: 10 * time.Second,
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
	filesLn, err := edge.ListenSocket(cfg.FilesSocket)
	if err != nil {
		_ = healthLn.Close()
		_ = publicLn.Close()
		slog.Error("files_socket_listen_failed", "path", cfg.FilesSocket, "error", err)
		stopReaper()
		<-reaperDone
		return err
	}
	slog.Info("files_socket_listening", "path", cfg.FilesSocket)
	mcpLn, err := edge.ListenSocket(cfg.MCPSocket)
	if err != nil {
		_ = healthLn.Close()
		_ = publicLn.Close()
		_ = filesLn.Close()
		slog.Error("mcp_socket_listen_failed", "path", cfg.MCPSocket, "error", err)
		stopReaper()
		<-reaperDone
		return err
	}
	slog.Info("mcp_socket_listening", "path", cfg.MCPSocket)

	serveErr := make(chan error, 4)
	go func() { serveErr <- fmt.Errorf("health listener: %w", health.Serve(healthLn)) }()
	go func() { serveErr <- fmt.Errorf("public listener: %w", public.Serve(publicLn)) }()
	go func() { serveErr <- fmt.Errorf("files socket: %w", filesSock.Serve(filesLn)) }()
	go func() { serveErr <- fmt.Errorf("mcp socket: %w", mcpSock.Serve(mcpLn)) }()
	// Every listener is bound, so a probe that sees ready can reach the routes,
	// ae-collab the Files socket and ae-design-agent the MCP socket.
	ready.PublicBound()
	ready.FilesSocketBound()
	ready.MCPSocketBound()

	var runErr error
	signaled := false
	select {
	case runErr = <-serveErr:
		slog.Error("listener_failed", "error", runErr)
	case <-ctx.Done():
		signaled = true
	}
	drainStart := time.Now()
	ready.Draining()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), listenerDrainTimeout)
	defer cancel()
	for _, srv := range []*http.Server{public, health} {
		if err := srv.Shutdown(shutdownCtx); err != nil {
			slog.Error("shutdown_failed", "addr", srv.Addr, "error", err)
			runErr = err
		}
	}
	// The sockets close last: on SIGTERM they keep accepting through
	// ae-collab's final flush and the agent's outbox drain, then drain the
	// requests in flight, concurrently.
	if signaled {
		time.Sleep(time.Until(drainStart.Add(socketDrainWindow)))
	}
	if err := shutdownSockets(filesSock, mcpSock); err != nil {
		runErr = err
	}
	// The listeners are drained, so no request can still be reading; the
	// reaper's git children die with its context.
	stopReaper()
	select {
	case <-reaperDone:
	case <-time.After(reaperStopTimeout):
		slog.Error("reaper_stop_timeout")
	}
	if runErr != nil {
		return runErr
	}
	slog.Info("stopped")
	return nil
}

// shutdownSockets shuts the sockets down concurrently under one
// socketShutdownTimeout and answers the last failure. Task 3.7's usage flush
// joins this group.
func shutdownSockets(socks ...*http.Server) error {
	ctx, cancel := context.WithTimeout(context.Background(), socketShutdownTimeout)
	defer cancel()
	errs := make(chan error, len(socks))
	for _, srv := range socks {
		go func() {
			err := srv.Shutdown(ctx)
			if err != nil {
				slog.Error("shutdown_failed", "addr", srv.Addr, "error", err)
			}
			errs <- err
		}()
	}
	var last error
	for range socks {
		if err := <-errs; err != nil {
			last = err
		}
	}
	return last
}
