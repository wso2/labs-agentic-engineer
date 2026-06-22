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
	"encoding/json"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
)

// apiVersion is the OpenAPI document version. Bump on contract changes.
const apiVersion = "1.0.0"

// newHumaAPI creates the Huma API over apiMux. apiMux is already wrapped by the
// JWT + orgensure middleware in NewHandler, so every Huma operation inherits
// user-JWT verification and JIT org provisioning; the per-route tenant gate is
// added by humakit.OrgScopedInput. Huma's built-in spec/docs routes stay at
// their defaults but are unreachable on apiMux (it only receives /api/*); the
// public spec + docs are served on the outer mux by registerHumaDocs.
func newHumaAPI(apiMux *http.ServeMux) huma.API {
	cfg := huma.DefaultConfig("ASDLC BFF API", apiVersion)
	// Disable Huma's built-in spec/docs/schema routes: they would land on
	// apiMux (unreachable — it only receives /api/*), and clearing SchemasPath
	// also suppresses the $schema response-body link injection so converted
	// endpoints stay byte-compatible with the existing console. The public spec
	// + docs are served on the outer mux by registerHumaDocs.
	cfg.OpenAPIPath = ""
	cfg.DocsPath = ""
	cfg.SchemasPath = ""
	cfg.Components.SecuritySchemes = map[string]*huma.SecurityScheme{
		"userJWT":      {Type: "http", Scheme: "bearer", BearerFormat: "JWT", Description: "Thunder end-user JWT (RS256, JWKS-verified)"},
		"taskJWT":      {Type: "http", Scheme: "bearer", BearerFormat: "JWT", Description: "BFF-signed per-task RS256 JWT (runner pods)"},
		"connectState": {Type: "apiKey", In: "query", Name: "state", Description: "Signed GitHub connect-state JWT"},
		"webhookHmac":  {Type: "apiKey", In: "header", Name: "X-Hub-Signature-256", Description: "GitHub webhook HMAC-SHA256 signature"},
	}
	return humago.New(apiMux, cfg)
}

// registerHumaDocs serves the generated OpenAPI document and the Stoplight
// Elements docs UI publicly on the outer mux (no JWT) so tooling and the
// console can read the spec without a token. The spec is rendered lazily at
// request time, so operations registered after this call are still included.
func registerHumaDocs(mux *http.ServeMux, api huma.API) {
	mux.HandleFunc("GET /openapi.yaml", func(w http.ResponseWriter, _ *http.Request) {
		b, err := api.OpenAPI().YAML()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/yaml")
		_, _ = w.Write(b)
	})
	mux.HandleFunc("GET /openapi.json", func(w http.ResponseWriter, _ *http.Request) {
		b, err := json.Marshal(api.OpenAPI())
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(b)
	})
	mux.HandleFunc("GET /docs", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(docsHTML))
	})
}

const docsHTML = `<!doctype html>
<html>
  <head>
    <meta charset="utf-8">
    <title>ASDLC BFF API</title>
    <meta name="viewport" content="width=device-width, initial-scale=1">
    <script src="https://unpkg.com/@stoplight/elements/web-components.min.js"></script>
    <link rel="stylesheet" href="https://unpkg.com/@stoplight/elements/styles.min.css">
  </head>
  <body>
    <elements-api apiDescriptionUrl="/openapi.yaml" router="hash" layout="sidebar"></elements-api>
  </body>
</html>`
