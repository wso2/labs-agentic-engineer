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

// Package aestudiotools is aep-api's adapter to an org's ae-studio-tools
// /internal/v1 (the per-org AE Studio pod's machine API), over the generated
// client in gen. Every call resolves the org's Target (endpoint.go), carries
// the AE-only M2M token (token.go) and X-Impersonate-Org = the pod's OU id,
// and maps the answer to the typed errors in errors.go. No retries, except
// one token refresh on a refused token.
package aestudiotools

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/wso2/aep/aep-api/internal/clients/aestudiotools/gen"
)

// RepoRef names a project's repository in an org: the org (its OpenChoreo
// namespace, which picks the pod) and the GitHub owner and repository.
type RepoRef struct{ Org, Owner, Repo string }

// defaultCallTimeout bounds a unary call (the gateway's /internal/v1 route
// allows 120 s). A turn stream is bounded by the caller's ctx only.
const defaultCallTimeout = 110 * time.Second

// Config wires an Adapter.
type Config struct {
	// Endpoints resolves an org's Target; the Adapter caches it (~30 s).
	Endpoints Endpoints
	// Tokens is the AE-only client's token source.
	Tokens TokenSource
	// HTTP carries the calls. Its Timeout is ignored: a turn stream runs up
	// to 30 min under the caller's ctx, and unary calls are bounded by
	// CallTimeout. Nil uses a clone of http.DefaultTransport (which waits for
	// 100 Continue before an upload).
	HTTP *http.Client
	// CallTimeout bounds each unary call. Zero means 110 s.
	CallTimeout time.Duration
}

// Adapter calls an org's ae-studio-tools. It implements Turns, References and
// Identity.
type Adapter struct {
	endpoints   *endpointCache
	tokens      TokenSource
	http        *http.Client
	callTimeout time.Duration

	missingOnce sync.Once
}

var (
	_ Turns      = (*Adapter)(nil)
	_ References = (*Adapter)(nil)
	_ Identity   = (*Adapter)(nil)
)

// New builds the Adapter. It does no I/O.
func New(cfg Config) *Adapter {
	hc := &http.Client{Transport: http.DefaultTransport.(*http.Transport).Clone()}
	if cfg.HTTP != nil {
		c := *cfg.HTTP
		c.Timeout = 0
		hc = &c
	}
	timeout := cfg.CallTimeout
	if timeout <= 0 {
		timeout = defaultCallTimeout
	}
	return &Adapter{endpoints: newEndpointCache(cfg.Endpoints), tokens: cfg.Tokens, http: hc, callTimeout: timeout}
}

// call sends one request with the generated client c; auth sets the bearer.
type call func(ctx context.Context, c *gen.Client, impersonateOrg string, auth gen.RequestEditorFn) (*http.Response, error)

// send runs fn against the org's pod and returns a 2xx response with its
// body open, or the mapped error. A refused token (401, or a 403 other than
// owner_not_allowed) is dropped and the call sent once more with a fresh one when
// replayable says the request can be sent again; refused again, the call is
// ErrAEStudioMisconfigured and logs ae_studio.auth_failed {org, status}.
func (a *Adapter) send(ctx context.Context, org, op string, fn call, replayable func() bool) (*http.Response, error) {
	target, err := a.endpoints.resolve(ctx, org)
	if err != nil {
		return nil, err
	}
	c, err := gen.NewClient(target.BaseURL, gen.WithHTTPClient(a.http))
	if err != nil {
		return nil, fmt.Errorf("%w: tools URL: %w", ErrAEStudioUnavailable, err)
	}
	for refreshed := false; ; refreshed = true {
		tok, err := a.tokens.Token(ctx)
		if err != nil {
			return nil, a.tokenFailed(ctx, org, err)
		}
		resp, err := fn(ctx, c, target.ImpersonateOrg, bearer(tok))
		if err != nil {
			return nil, a.transportFailed(ctx, org, op, err)
		}
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			return resp, nil
		}
		ans := readAnswer(resp)
		if !ans.authRefused() {
			if ans.gatewayGone() {
				a.endpoints.drop(org)
			}
			return nil, ans.toError(op)
		}
		a.tokens.Invalidate()
		if refreshed {
			slog.ErrorContext(ctx, "ae_studio.auth_failed", "org", org, "status", ans.status)
			return nil, fmt.Errorf("%w: %s answered %d to a fresh token", ErrAEStudioMisconfigured, op, ans.status)
		}
		if !replayable() {
			return nil, fmt.Errorf("%w: %s refused the token after the body was sent; send it again", ErrAEStudioUnavailable, op)
		}
	}
}

// always is the replayable of a request whose body is rebuilt on each send.
func always() bool { return true }

// bearer sets the AE-only client's token on a request.
func bearer(tok string) gen.RequestEditorFn {
	return func(_ context.Context, req *http.Request) error {
		req.Header.Set("Authorization", "Bearer "+tok)
		return nil
	}
}

// tokenFailed logs a misconfigured AE-only client, value-free: missing
// credentials once per process (C5, Q-13), a refused client every time.
func (a *Adapter) tokenFailed(ctx context.Context, org string, err error) error {
	switch {
	case errors.Is(err, errClientCredentialsMissing):
		a.missingOnce.Do(func() {
			slog.ErrorContext(ctx, "ae_studio.misconfigured", "org", org, "reason", "client_credentials_missing")
		})
	case errors.Is(err, errTokenRefused):
		slog.ErrorContext(ctx, "ae_studio.misconfigured", "org", org, "reason", "token_refused")
	}
	return err
}

// transportFailed maps a request that got no answer. The caller's own
// context ending it (a cancellation, or the caller's deadline: the kickoff's
// budget, an activity's timeout) is the caller's and is returned as is.
// Anything else (a dial error, a broken connection, the Adapter's own call
// timeout) is ErrAEStudioUnavailable and drops the org's cached Target,
// which may be stale.
func (a *Adapter) transportFailed(ctx context.Context, org, op string, err error) error {
	if ctx.Err() != nil && !errors.Is(context.Cause(ctx), errCallTimeout) {
		return fmt.Errorf("ae studio: %s: %w", op, ctx.Err())
	}
	a.endpoints.drop(org)
	return fmt.Errorf("%w: %s: %w", ErrAEStudioUnavailable, op, err)
}

// errCallTimeout is the cause of a unary call cut by the call timeout, which
// tells the Adapter's deadline from the caller's.
var errCallTimeout = errors.New("ae studio: call timeout")

// unary bounds a unary call by the call timeout.
func (a *Adapter) unary(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeoutCause(ctx, a.callTimeout, errCallTimeout)
}

// validRef refuses a RepoRef the pod could not route.
func validRef(ref RepoRef) error {
	if ref.Org == "" || ref.Owner == "" || ref.Repo == "" {
		return fmt.Errorf("ae studio: incomplete repo ref %+v", ref)
	}
	return nil
}
