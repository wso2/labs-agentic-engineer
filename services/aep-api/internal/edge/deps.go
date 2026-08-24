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

package edge

import (
	deliveryhttpapi "github.com/wso2/aep/aep-api/internal/delivery/httpapi"
	dephttpapi "github.com/wso2/aep/aep-api/internal/dependencies/httpapi"
	opshttpapi "github.com/wso2/aep/aep-api/internal/ops/httpapi"
	orghttpapi "github.com/wso2/aep/aep-api/internal/organization/httpapi"
	"github.com/wso2/aep/aep-api/internal/platform/auth"
	projectshttpapi "github.com/wso2/aep/aep-api/internal/projects/httpapi"
	schttpapi "github.com/wso2/aep/aep-api/internal/sourcecontrol/httpapi"
	spechttpapi "github.com/wso2/aep/aep-api/internal/spec/httpapi"
)

// Deps carries every feature service the strict handlers (handlers_*.go)
// call — nothing more: a field here means at least one handler reads it
// (fields whose only consumers were the retired Huma registrations were
// dropped at the extraction). main.go (internal/app) fills it with the real
// services; component tests fill only what the feature under test needs
// (untouched fields nil-guard or 503 in their handlers).
type Deps struct {
	TaskTokens *auth.TaskTokenManager

	// PublisherTokens verifies Thunder publisher CC JWTs on the MCP mount
	// (AgentsScopedVerifier fallback). Nil keeps MCP BFF-only (local/tests).
	PublisherTokens *auth.PublisherTokenVerifier

	// DesignSvc is the narrow design-dependency reader backing the edge's own
	// GET /projects/{name}/design/dependencies handler (handlers_design.go) —
	// the one op the edge serves directly rather than promoting from a domain
	// embed. *spec.designService satisfies it structurally. Nil answers 503.
	DesignSvc designDependencyReader

	// Ops is the FIRST landed domain (P1): its handlers are embedded straight
	// into apiServer, so the edge holds no ops service and no ops handler file.
	// Every later domain arrives the same way, and this bag shrinks to nothing
	// by P9 — it is the legacy handlers' dependency bag, not the edge's.
	Ops           *opshttpapi.Handlers
	SourceControl *schttpapi.Handlers
	Organization  *orghttpapi.Handlers
	Spec          *spechttpapi.Handlers
	Projects      *projectshttpapi.Handlers
	Delivery      *deliveryhttpapi.Handlers
	Dependencies  *dephttpapi.Handlers
}
