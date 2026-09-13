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

package identity

// target.go — WHICH identity provider a role or a test user belongs to.
//
// There is no longer one directory. Every environment of every org has its own
// identity provider — the environment tier, "T2" — and a login minted on one is
// rejected by every other. So nothing in this domain may name a directory
// without also naming the (org, environment) it belongs to: the shared scope is
// that pair, not the cluster.
//
// The resolver is a PORT, like every other outbound edge here, so this domain
// still names no client package. The composition root reads the binding the
// environment's Thunder was recorded under, fetches its admin credential, and
// hands back a Directory already bound to it.

import "context"

// Scope is the (org, environment) pair that names one identity provider, and
// with it the rows this domain keeps about that provider's objects.
//
// It travels as one value rather than two strings because every store method
// takes it and nothing here is ever correct with only one half: an org without
// an environment, or the reverse, addresses no directory at all.
type Scope struct {
	OrgID       string
	Environment string
}

// String renders the pair for a log line or an error — "acme/default".
func (s Scope) String() string { return s.OrgID + "/" + s.Environment }

// Target is one environment's identity provider: the directory to write, and
// the issuer a login it mints is valid at.
//
// Issuer is carried because it is the only half of the binding a HUMAN needs.
// The gate publishes test-user logins on a ticket, and a login with no issuer
// beside it is unusable the moment more than one identity provider exists —
// which is the whole point of this tier.
type Target struct {
	// OrgID and Environment are the pair this directory serves, and the pair
	// every row this domain writes is keyed by.
	OrgID       string
	Environment string
	// Issuer is the identity provider's public issuer, e.g.
	// http://default-idp.amp.localhost:8080.
	Issuer string
	// Directory is the admin surface, already authenticated for this target.
	Directory Directory
}

// Scope is the key every row about this target's objects carries.
func (t Target) Scope() Scope { return Scope{OrgID: t.OrgID, Environment: t.Environment} }

// TargetResolver answers "which identity provider serves this org".
//
// It takes the ORG only, and not the environment, on purpose. A build's roles
// and test users belong to the environment that build is validated in, and
// today every build deploys and validates in exactly one — so the environment is
// chosen in ONE place, the resolver's own construction at the composition root
// (`app.newIdentityTargetResolver`), rather than at each of the four call sites
// here. When a run carries its own environment, this method grows a parameter
// and the domain passes it through; nothing else about this design moves.
//
// The environment it chose comes back on every Target, so a log line, an error
// and a gate comment can always name it.
//
// Two methods, split by whether they can FAIL. Scope is the choice alone and is
// pure; Resolve reads the environment's binding and its admin credential over
// the network. The panel needs that split: its read degrades to the platform's
// own record when the identity provider is unreachable, and it cannot ask the
// store for that record without first knowing which environment's rows to ask
// for.
type TargetResolver interface {
	// Scope names the (org, environment) whose identity provider serves this
	// org. Pure — no I/O, and it cannot fail.
	Scope(orgID string) Scope
	// Resolve returns that environment's directory, bound and authenticated.
	Resolve(ctx context.Context, orgID string) (Target, error)
}
