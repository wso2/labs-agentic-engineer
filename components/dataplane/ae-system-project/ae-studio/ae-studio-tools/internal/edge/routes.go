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

	"github.com/wso2/aep/ae-studio-tools/internal/auth"
	"github.com/wso2/aep/ae-studio-tools/internal/config"
	"github.com/wso2/aep/ae-studio-tools/internal/github"
	"github.com/wso2/aep/ae-studio-tools/internal/problem"
)

// Deps is what the public listener's routes need. main.go builds it from a
// config.Load result: the gates panic on empty security parameters.
type Deps struct {
	Cfg      config.Config
	Verifier *auth.Verifier
	GitHub   github.Identity
	// Webhook serves POST /webhooks/github (Task 1.4); it must not be nil.
	Webhook http.Handler
}

// Routes is the public listener's mount table (ticket 04 §1): one row per
// route group, each listener → gate → handler. Every gate runs before route
// matching inside its group, so an unknown path is 401/403 before it is 404.
// The health probes are on the health listener only. A path not listed here
// is 404. Sockets are separate listeners added in later phases.
func Routes(d Deps) http.Handler {
	userGate := auth.UserGate(d.Verifier, d.Cfg.UserAudiences, d.Cfg.OrgID, d.Cfg.OrgHandle)
	m2mGate := auth.M2MGate(d.Verifier, d.Cfg.M2MClientID, d.Cfg.OrgID)

	mux := http.NewServeMux()
	// /v1: browser, Platform IdP user JWT of the pod's org (operations land
	// with the git engine).
	mux.Handle("/v1/", userGate(v1Handler()))
	// /internal/v1: aep-api, AE-only M2M + X-Impersonate-Org. Body cap →
	// gate → validator → generated server; every request is access-logged.
	mux.Handle("/internal/v1/", accessLog(capBody(internalBodyBytes, m2mGate(internalHandler(d.GitHub)))))
	// /webhooks/github: GitHub, HMAC signature (Task 1.4).
	mux.Handle("POST /webhooks/github", d.Webhook)
	mux.Handle("/", http.HandlerFunc(notFound))
	return mux
}

func notFound(w http.ResponseWriter, _ *http.Request) {
	problem.Write(w, http.StatusNotFound, "not_found", "no such route")
}
