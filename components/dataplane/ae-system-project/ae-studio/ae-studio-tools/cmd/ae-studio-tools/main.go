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
// reporting ready once the public listener and both sockets are bound. The
// turns aep-api starts on the public listener are relayed to
// ae-design-agent's Turn socket (AE_TURN_SOCKET, turns.Relay), and the
// records of finished turns the agent hands in over the MCP socket go to
// aep-api through the usage outbox (usage.Sender).
// SIGTERM (tini forwards it) drains the public and health listeners, keeps
// both sockets serving through the drain window for ae-collab's final flush
// and the agent's outbox drain (07 §10), shuts them down while it flushes the
// usage outbox, then stops the reaper.
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
	"github.com/wso2/aep/ae-studio-tools/internal/turns"
	"github.com/wso2/aep/ae-studio-tools/internal/usage"
	"github.com/wso2/aep/ae-studio-tools/internal/webhook"
)

// reaperStopTimeout bounds the wait for the reaper after its context is
// canceled (its git children are killed with it), separate from the listener
// drain's budget.
const reaperStopTimeout = 2 * time.Second

// Shutdown budget, inside the pod's 30 s termination grace (Q-5/D-5):
//
//	socketDrainWindow (10 s)
//	+ max(socketShutdownTimeout (15 s), usageFlushTimeout (3 s))
//	+ reaperStopTimeout (2 s) = 27 s < 30 s
//
// The public and health listeners drain (listenerDrainTimeout, 5 s) inside the
// window. Both sockets keep accepting through the one window (the agent hands
// its shutdown turn's record in during it) and are then shut down
// concurrently, with the usage flush beside them, so neither the second
// socket nor the flush adds to the total.
const (
	// listenerDrainTimeout bounds the public and health listeners' drain. A
	// relayed turn is a request in flight: the agent gets SIGTERM at the
	// same moment and ends it with result failed/shutdown, which ends the
	// relay inside this drain.
	listenerDrainTimeout = 5 * time.Second
	// socketDrainWindow is how long after SIGTERM the Files and MCP sockets
	// keep accepting: ae-collab and ae-design-agent get SIGTERM at the same
	// moment, ae-collab flushes every Room through the Files socket and the
	// agent drains its outbox through the MCP socket (07 §10). Coupled to
	// ae-collab's shutdown flush budget (SHUTDOWN_FLUSH_BUDGET_MS, 8 s,
	// ae-collab/src/committer.ts) and the agent's handover
	// (SHUTDOWN_HANDOVER_MS, ae-design-agent/src/pod/shutdown.ts: turn abort
	// ≤ 2 s plus outbox drain, ≤ 8 s in all from SIGTERM). Both end ≤ 8 s
	// after SIGTERM, a 2 s margin inside this window; raise them together.
	socketDrainWindow = 10 * time.Second
	// socketShutdownTimeout bounds the wait for requests still in flight on
	// either socket once they stop accepting.
	socketShutdownTimeout = 15 * time.Second
	// usageFlushTimeout bounds the final usage send (07 §10): one or more
	// record-turn-usage calls, run beside the sockets' shutdown.
	usageFlushTimeout = 3 * time.Second
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
	resolver := projects.NewAEPAPIResolver(aepAPI)
	// The usage outbox: finished turns' records to aep-api's
	// record-turn-usage, as the publisher, for the process lifetime.
	usageOutbox := startUsageOutbox(usage.New(usage.NewAEPAPIPost(aepAPI)))
	defer usageOutbox.stopRun()
	reader := files.Reader{Engine: engine, Projects: resolver}
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
		Usage:     usageOutbox.sender,
	}
	// The GitHub client over the gitpat: commit identity today, and the
	// hooks it registers deliver to AE_WEBHOOK_URL signed with the webhook
	// secret.
	gh := github.New(github.Config{
		Token:      func(context.Context) (string, error) { return cfg.GitHubPAT, nil },
		HookURL:    cfg.WebhookURL,
		HookSecret: cfg.WebhookSecret,
	})
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
			Cfg:        cfg,
			Verifier:   auth.NewVerifier(cfg.IDPIssuer, auth.NewJWKSCache(cfg.IDPJWKSURL)),
			GitHub:     gh,
			Webhook:    edge.WebhookHandler(cfg.WebhookSecret, webhook.Unwired()),
			Files:      reader,
			References: engine,
			Projects:   resolver,
			// The turns aep-api starts run on the agent's Turn socket.
			Turns: turns.Relay{Turns: turns.NewClient(cfg.TurnSocket)},
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
	// requests in flight, concurrently, while the usage outbox sends what
	// is left.
	if signaled {
		time.Sleep(time.Until(drainStart.Add(socketDrainWindow)))
	}
	if err := shutdownSockets(serverShutdown(filesSock), serverShutdown(mcpSock), usageOutbox.flush); err != nil {
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

// shutdownSockets runs the shutdown steps (the sockets' and the usage
// flush) concurrently under one socketShutdownTimeout and answers the last
// failure.
func shutdownSockets(steps ...func(context.Context) error) error {
	ctx, cancel := context.WithTimeout(context.Background(), socketShutdownTimeout)
	defer cancel()
	errs := make(chan error, len(steps))
	for _, step := range steps {
		go func() { errs <- step(ctx) }()
	}
	var last error
	for range steps {
		if err := <-errs; err != nil {
			last = err
		}
	}
	return last
}

// serverShutdown is srv's shutdown step, logging a failure.
func serverShutdown(srv *http.Server) func(context.Context) error {
	return func(ctx context.Context) error {
		err := srv.Shutdown(ctx)
		if err != nil {
			slog.Error("shutdown_failed", "addr", srv.Addr, "error", err)
		}
		return err
	}
}

// usageOutbox is the usage sender running in the background for the
// process lifetime.
type usageOutbox struct {
	sender *usage.Sender
	cancel context.CancelFunc
	done   chan struct{}
}

func startUsageOutbox(s *usage.Sender) *usageOutbox {
	ctx, cancel := context.WithCancel(context.Background())
	o := &usageOutbox{sender: s, cancel: cancel, done: make(chan struct{})}
	go func() {
		defer close(o.done)
		s.Run(ctx)
	}()
	return o
}

// stopRun stops the background delivery and waits for it; a send in flight
// is canceled and its records stay pending. Idempotent.
func (o *usageOutbox) stopRun() {
	o.cancel()
	<-o.done
}

// flush is the shutdown step: stop the background delivery, then send
// everything pending at once, bounded by usageFlushTimeout. Records the
// agent hands in after it began are not sent (its outbox drain ends inside
// the drain window, before it).
func (o *usageOutbox) flush(ctx context.Context) error {
	o.stopRun()
	ctx, cancel := context.WithTimeout(ctx, usageFlushTimeout)
	defer cancel()
	if err := o.sender.Flush(ctx); err != nil {
		slog.Error("usage.flush_failed", "error", err)
		return err
	}
	return nil
}
