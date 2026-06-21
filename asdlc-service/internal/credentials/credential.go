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

// Package credentials is the single seam for GitHub authentication in
// git-service. Every code path that calls GitHub or runs `git` against a
// remote routes through Resolver.Resolve(ocOrgID) to obtain a Credential,
// then asks the credential for a token, identity, repo-owner, or webhook
// strategy as needed.
//
// Implementations cover App-installation and per-org user-PAT kinds; call
// sites stay identical because they consume the polymorphic Credential
// surface and never branch on the kind.
//
// Three architectural rules these types enforce:
//
//   1. No call site type-switches on Credential.
//   2. No call site reads identity, repo-owner, or token from any other
//      source — not env, not the GitRepository row.
//   3. Every external GitHub operation passes ocOrgID explicitly. Resolvers
//      refuse an empty ocOrgID.
package credentials

import (
	"context"
	"errors"
	"time"
)

// Credential is a polymorphic surface over the ways the platform can
// authenticate to GitHub (App-installation and per-org user-PAT).
//
// Callers MUST NOT type-switch on the implementation.
type Credential interface {
	// Token returns a usable GitHub token and the time at which it stops
	// being valid. Long-lived kinds may return time.Time{} (zero) to
	// indicate "never expires" — callers treat zero as "no refresh needed".
	Token(ctx context.Context) (token string, expiresAt time.Time, err error)

	// Identity returns the committer attribution this credential maps to.
	Identity() Identity

	// RepoOwner returns the GitHub org/user login under which new repos are
	// provisioned. App mode: the install's account login. PAT mode: the
	// GitHub org chosen at connect time.
	RepoOwner() string

	// WebhookStrategy says how the platform should arrange event delivery
	// for repos using this credential. Some kinds answer "register a
	// per-repo hook"; others answer "rely on platform-level delivery, do
	// nothing." Callers dispatch the strategy without inspecting which
	// kind it is.
	WebhookStrategy() WebhookStrategy
}

// Identity is the committer attribution surfaced by a Credential. The Login
// field is the GitHub user/bot login (used for hosts.yml + audit); Name and
// Email are what go on git commit author/committer headers.
type Identity struct {
	Name  string
	Email string
	Login string
}

// WebhookStrategy enumerates how the platform arranges event delivery for
// repos backed by a given Credential.
type WebhookStrategy int

const (
	// WebhookPerRepo says: register a webhook on each repo at provision time.
	// User-PAT mode uses this strategy.
	WebhookPerRepo WebhookStrategy = iota
	// WebhookPlatform says: event delivery is platform-wide (a GitHub App's
	// configured callback). App-installation mode uses this strategy.
	WebhookPlatform
)

// Resolver resolves the credential for a given organisation by looking up
// its per-org connection record. ocOrgID is MANDATORY — every external
// GitHub op names the org it acts for.
type Resolver interface {
	Resolve(ctx context.Context, ocOrgID string) (Credential, error)
}

// ErrEmptyOcOrgID is returned by resolvers when an empty ocOrgID is passed.
// This is the multi-tenant invariant — every external GitHub op names the
// org it acts for.
var ErrEmptyOcOrgID = errors.New("credentials: ocOrgID is required")
