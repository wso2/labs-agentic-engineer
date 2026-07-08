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

package orchestration

import (
	"crypto/subtle"
	"strings"

	"github.com/danielgtaylor/huma/v2"
)

// This file holds the by-construction auth gate for the orchestration internal
// S2S ops. Unlike the execution runner-callback surface (auth.ExecutionScopedInput,
// which is execution-id-scoped and verifies a per-task/publisher-cc bearer), the
// orchestration ops are org/project-scoped with no execution id in their path —
// they are the orchestrator worker calling back into aep-api, not a runner pod.
// So they authenticate with a single shared secret: the same value the
// orchestrator sends as AEP_API_INTERNAL_BEARER on every call
// (services/orchestrator/internal/activities/http_client.go), verified here.

// sharedBearer is the process-wide secret every orchestration internal op
// checks. Set once at composition via SetInternalBearer — never from a test
// harness — so there is no concurrent write, mirroring
// auth.SetRunnerAuthorizer's wiring discipline.
var sharedBearer string

// SetInternalBearer wires the shared secret at composition time. An empty
// secret leaves the surface unauthenticated (every request gets a 503) —
// acceptable only for local loopback dev; non-local deployments must set it.
func SetInternalBearer(secret string) { sharedBearer = secret }

// bearerAuth is embedded by every orchestration internal-op input struct to
// make the op authenticated-by-construction: Resolve runs before the handler
// and rejects the request unless the Authorization header carries the
// configured shared bearer.
type bearerAuth struct{}

var _ huma.Resolver = (*bearerAuth)(nil)

// Resolve verifies the Authorization header against the shared bearer.
func (bearerAuth) Resolve(ctx huma.Context) []error {
	if sharedBearer == "" {
		return []error{huma.Error503ServiceUnavailable("orchestration internal auth not configured")}
	}
	const prefix = "Bearer "
	header := ctx.Header("Authorization")
	if len(header) <= len(prefix) || !strings.EqualFold(header[:len(prefix)], prefix) {
		return []error{huma.Error401Unauthorized("bearer token required")}
	}
	token := header[len(prefix):]
	if subtle.ConstantTimeCompare([]byte(token), []byte(sharedBearer)) != 1 {
		return []error{huma.Error401Unauthorized("invalid bearer")}
	}
	return nil
}
