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

package api

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"

	"github.com/wso2/aep/aep-api/internal/feature/orgcreds"
)

// newInternalAPI creates the Huma API for the internal service-to-service
// surface over internalMux. It is the sibling of newHumaAPI (the public edge),
// with three deliberate differences: (1) it is NOT wrapped by the user-JWT
// middleware — each operation authenticates by construction via
// auth.ExecutionScopedInput (BFF Task-JWT / publisher-cc); (2) its security
// schemes are the S2S ones, not userJWT; (3) its spec is non-public — it is
// generated to a checked-in file and is never advertised on the gateway. See
// docs/design/internal-s2s-api.md §3.
func newInternalAPI(internalMux *http.ServeMux) huma.API {
	cfg := huma.DefaultConfig("AEP Internal S2S API", apiVersion)
	// Same rationale as newHumaAPI: clear the built-in spec/docs/schema routes
	// (they would land on internalMux, which only receives /internal/* anyway)
	// and suppress the $schema response-body link so runner callbacks stay
	// byte-compatible with the pre-Huma raw handlers.
	cfg.OpenAPIPath = ""
	cfg.DocsPath = ""
	cfg.SchemasPath = ""
	cfg.Components.SecuritySchemes = map[string]*huma.SecurityScheme{
		"taskJWT":     {Type: "http", Scheme: "bearer", BearerFormat: "JWT", Description: "BFF-signed per-task RS256 JWT (runner pods)"},
		"publisherCC": {Type: "http", Scheme: "bearer", BearerFormat: "JWT", Description: "Thunder per-org publisher client-credentials token"},
	}
	return humago.New(internalMux, cfg)
}

// InternalDeps carries the services the internal S2S operations need. main.go
// fills it with real services; GenerateInternalOpenAPIYAML passes the zero
// value (nil deps — registration never invokes them).
type InternalDeps struct {
	CredsRefresh orgcreds.CredentialsRefreshService
}

// RegisterAllInternal registers every internal S2S operation on the Huma API.
// The single canonical list — used by NewHandler (real deps) and the internal
// spec generator (zero deps).
func RegisterAllInternal(api huma.API, d InternalDeps) {
	orgcreds.RegisterInternalCredentials(api, d.CredsRefresh)
}

// GenerateInternalOpenAPIYAML builds the internal S2S OpenAPI document and
// returns it as YAML. Used by the `make openapi` generator and the internal
// spec-freshness drift guard. Deps are nil — registration is pure metadata.
func GenerateInternalOpenAPIYAML() ([]byte, error) {
	internalMux := http.NewServeMux()
	api := newInternalAPI(internalMux)
	RegisterAllInternal(api, InternalDeps{})
	return api.OpenAPI().YAML()
}
