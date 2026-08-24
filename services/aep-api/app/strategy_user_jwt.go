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

	"github.com/wso2/aep/aep-api/ocauth"
)

// UserJWTStrategy forwards the inbound user JWT to OpenChoreo
// (AuthModeUserJWT) for requests that carry one and aren't explicitly marked
// service-identity. Calls explicitly marked via ocauth.WithServiceIdentity —
// background jobs, webhook handlers, Temporal activities, orchestration —
// use AuthModeServiceM2M instead.
//
// Deliberately fails closed: a call with NEITHER a service-identity marker
// NOR a user JWT gets AuthModeNone (no bearer at all, so OC rejects it)
// rather than silently falling back to the BFF's own privileged service
// identity. The alternative — treating "no token" as an implicit request for
// M2M — would let anyone reach OC with the BFF's own privileges just by
// omitting the Authorization header on any route whose auth enforcement is
// missing, misconfigured, or added later without realizing it feeds OC.
// Every legitimate no-user-JWT call path (webhook receiver, Temporal
// activities, orchestration) must call ocauth.WithServiceIdentity explicitly;
// there is no implicit path to service identity.
//
// Forwarding the user JWT only reaches OpenChoreo if the target cluster's
// authz config actually grants that JWT's claims a role (see
// deployments/helm-charts/platform/templates/aep-api/authz.yaml); this
// strategy makes no such guarantee on its own.
type UserJWTStrategy struct{}

// Decide implements ocauth.RequestAuthStrategy.
func (UserJWTStrategy) Decide(ctx context.Context) ocauth.AuthMode {
	if ocauth.IsServiceIdentity(ctx) {
		return ocauth.AuthModeServiceM2M
	}
	if ocauth.GetAuthToken(ctx) == "" {
		return ocauth.AuthModeNone
	}
	return ocauth.AuthModeUserJWT
}

var _ ocauth.RequestAuthStrategy = UserJWTStrategy{}
