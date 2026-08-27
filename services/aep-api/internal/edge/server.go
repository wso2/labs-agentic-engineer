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
	"net/http"

	deliveryhttpapi "github.com/wso2/aep/aep-api/internal/delivery/httpapi"
	dephttpapi "github.com/wso2/aep/aep-api/internal/dependencies/httpapi"
	"github.com/wso2/aep/aep-api/internal/gen"
	identityhttpapi "github.com/wso2/aep/aep-api/internal/identity/httpapi"
	opshttpapi "github.com/wso2/aep/aep-api/internal/ops/httpapi"
	orghttpapi "github.com/wso2/aep/aep-api/internal/organization/httpapi"
	"github.com/wso2/aep/aep-api/internal/platform/httpkit"
	projectshttpapi "github.com/wso2/aep/aep-api/internal/projects/httpapi"
	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
	schttpapi "github.com/wso2/aep/aep-api/internal/sourcecontrol/httpapi"
	spechttpapi "github.com/wso2/aep/aep-api/internal/spec/httpapi"
)

// apiServer implements the generated strict interface (gen.StrictServerInterface)
// for the public /api/v1 edge by METHOD PROMOTION ONLY — it declares no methods
// of its own. Every operation is promoted from exactly one domain
// embed, all at equal depth (apiServer → <domain>/httpapi.Handlers → <slice>.Handler):
//
//	*<domain>/httpapi.Handlers   one embed per domain
//
// TestMethodOrigin pins WHICH embed each op comes from, so a duplicate (an op
// served by two domains) fails the build (`ambiguous selector`) and a moved op
// silently fails the test. There is no legacyShim wrapper: every op resolves
// directly to its owning domain.
type apiServer struct {
	*opsHandlers           // P1 — ops (Incident RCA)
	*sourcecontrolHandlers // P2 — sourcecontrol (Source Control & Webhooks)
	*organizationHandlers  // P3 — organization (Org Config & Organizations)
	*specHandlers          // P4 — spec (Spec Authoring & Versioning)
	*deliveryHandlers      // P6 — delivery (Build, Tasks & Task-log stream)
	*projectsHandlers      // P7 — projects (Projects, Components, Builds & Config)
	*dependenciesHandlers  // P8 — dependencies (Provisioning, Resources & Access)
	*identityHandlers      // identity (the console's Security panel)

	// designSvc backs ListDesignDependencies (handlers_design.go), the single op
	// the edge serves via a method of its own rather than a domain embed: the
	// read-only design-dependency-status surface reads across domains, so it is
	// homed on the composite itself. Nil answers 503.
	designSvc designDependencyReader
}

// An embedded field is named by its UNQUALIFIED type name, so every domain's
// *httpapi.Handlers would collide as "Handlers". Local aliases give distinct
// field names while each domain keeps the clean, unstuttering type name (§6).
// One alias per landed domain.
type (
	opsHandlers           = opshttpapi.Handlers
	sourcecontrolHandlers = schttpapi.Handlers
	organizationHandlers  = orghttpapi.Handlers
	specHandlers          = spechttpapi.Handlers
	deliveryHandlers      = deliveryhttpapi.Handlers
	projectsHandlers      = projectshttpapi.Handlers
	dependenciesHandlers  = dephttpapi.Handlers
	identityHandlers      = identityhttpapi.Handlers
)

// Proves the METHOD SET only — never the wiring: it uses a nil pointer, so a
// nil sub-handler inside a Module still satisfies this and panics at runtime.
// Non-nil wiring is asserted by the per-domain assembly tests.
var _ gen.StrictServerInterface = (*apiServer)(nil)

// newAPIV1Handler assembles the whole contract-first serving chain for the
// public edge, innermost first:
//
//	strict impl (apiServer)               promotion-only composite: one embed per domain
//	→ tenant gate                          deny-by-default, tenant_gate.go
//	→ strict wrapper                       generated; envelope error writers
//	→ generated std ServeMux router        one pattern per contract operation
//	→ read-file catch-all                  nested {path} segments (see below)
//	→ request validator                    kin-openapi against the contract
//
// The caller mounts the result under the outer jwt → orgensure → gate-mode
// middleware (mountSurfaces), exactly where the Huma mux used to sit.
func newAPIV1Handler(deps Deps) http.Handler {
	strict := gen.NewStrictHandlerWithOptions(
		&apiServer{
			opsHandlers:           deps.Ops,
			sourcecontrolHandlers: sourceControlOrEmpty(deps.SourceControl),
			organizationHandlers:  deps.Organization,
			specHandlers:          deps.Spec,
			deliveryHandlers:      deps.Delivery,
			projectsHandlers:      deps.Projects,
			dependenciesHandlers:  dependenciesOrEmpty(deps.Dependencies),
			identityHandlers:      identityOrEmpty(deps.Identity),
			designSvc:             deps.DesignSvc,
		},
		[]gen.StrictMiddlewareFunc{tenantGate},
		gen.StrictHTTPServerOptions{
			RequestErrorHandlerFunc:  writeRequestError,
			ResponseErrorHandlerFunc: writeResponseError,
		},
	)

	mux := http.NewServeMux()
	gen.HandlerWithOptions(strict, gen.StdHTTPServerOptions{
		BaseURL:          httpkit.APIV1,
		BaseRouter:       mux,
		ErrorHandlerFunc: writeRequestError,
	})

	// read-file's {path} is a trailing wildcard (documented in the contract):
	// the generated single-segment pattern can't match nested spec paths, so
	// the same wrapped handler is also registered under the ServeMux catch-all.
	// Single-segment requests keep hitting the generated pattern (more
	// specific); multi-segment ones land here. PathValue("path") serves both.
	siw := &gen.ServerInterfaceWrapper{Handler: strict, ErrorHandlerFunc: writeRequestError}
	mux.HandleFunc("GET "+httpkit.APIV1+"/projects/{projectName}/files/{path...}", siw.ReadFile)

	return capRequestBody(requestValidator(mux))
}

// sourceControlOrEmpty keeps the harness contract Deps documents: a component
// test wires only the feature under test, and an unwired surface answers 503
// rather than panicking.
//
// A domain is embedded as a POINTER, so an unwired domain is a nil embed and any
// of its ops panics — a 500 where the pre-migration handler nil-guarded to 503.
// Assembling the domain with zero Deps restores it: sourcecontrol's ports are
// nil-tolerant by design, so every slice degrades to 503 exactly as before.
// (ops is different and deliberately so: its pre-migration handlers had no nil
// guard either, so it keeps failing loudly.)
func sourceControlOrEmpty(h *sourcecontrolHandlers) *sourcecontrolHandlers {
	if h != nil {
		return h
	}
	empty, err := schttpapi.New(sourcecontrol.Deps{})
	if err != nil {
		// Unreachable: zero Deps is a supported configuration for this domain.
		panic("api: assembling an empty sourcecontrol domain: " + err.Error())
	}
	return empty
}

// dependenciesOrEmpty is the same harness-contract guard for the dependencies
// domain: its pre-migration provisioning/resource-type handlers 503'd on a nil
// service, so a component test that leaves the domain unwired must keep getting
// 503 rather than a nil-embed panic. Both slices are nil-tolerant, so zero Deps
// degrades every op to 503 exactly as before.
func dependenciesOrEmpty(h *dependenciesHandlers) *dependenciesHandlers {
	if h != nil {
		return h
	}
	empty, err := dephttpapi.New(dephttpapi.Deps{})
	if err != nil {
		// Unreachable: zero Deps is a supported configuration for this domain.
		panic("api: assembling an empty dependencies domain: " + err.Error())
	}
	return empty
}

// identityOrEmpty is the same harness-contract guard for the identity domain:
// its slice is nil-tolerant, so a component test that leaves the domain unwired
// gets 503 from every Security-panel op rather than a nil-embed panic.
func identityOrEmpty(h *identityHandlers) *identityHandlers {
	if h != nil {
		return h
	}
	// NewEmpty, not New with a zero Deps: the domain's New REFUSES a nil panel
	// (a production wiring defect), and the unwired shape the harness wants is
	// its own named constructor rather than something a lax validator lets by.
	return identityhttpapi.NewEmpty()
}

// maxBodyBytes is the edge-wide request-body ceiling (413 beyond it), sized to
// the largest legitimate request: the reference-document upload (#383) — 10
// files × 5 MiB as multipart, so 50 MiB of raw bytes plus part headers. It was
// ~67 MiB while that upload rode base64 JSON; the ceiling stays at 80 MiB
// rather than tracking the drop, because shrinking a limit nobody is hitting
// only buys a future 413. body_cap_test.go pins the arithmetic so it can never
// quietly sink below the contract again. Per-file limits stay with the handlers
// (5 MiB in the references handler and the files service; import-skill keeps
// its own tighter one). The console's nginx proxy carries a matching
// client_max_body_size — the transport admits what the contract permits, at
// every hop.
const maxBodyBytes = 80 << 20

// capRequestBody bounds every request body before the validator (the first
// reader) touches it; an oversized body surfaces as *http.MaxBytesError and
// answers 413 (writeValidationError's dedicated branch).
func capRequestBody(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Body != nil {
			r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
		}
		next.ServeHTTP(w, r)
	})
}
