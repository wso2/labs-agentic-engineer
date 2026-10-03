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

// Package edge holds ae-studio-tools' HTTP surfaces.
package edge

import (
	"net/http"
	"sync/atomic"
)

// Readiness is the container's /readyz state (08 §2): ready once the public
// listener, the Files socket and the MCP socket are all bound, and not ready
// again from the moment the container starts draining. Safe for concurrent
// use.
type Readiness struct {
	public, filesSocket, mcpSocket, draining atomic.Bool
}

// PublicBound records that the public listener is bound.
func (r *Readiness) PublicBound() { r.public.Store(true) }

// FilesSocketBound records that the Files socket is bound.
func (r *Readiness) FilesSocketBound() { r.filesSocket.Store(true) }

// MCPSocketBound records that the MCP socket is bound.
func (r *Readiness) MCPSocketBound() { r.mcpSocket.Store(true) }

// Draining records that shutdown has begun.
func (r *Readiness) Draining() { r.draining.Store(true) }

// Ready reports whether a probe should see the container ready.
func (r *Readiness) Ready() bool {
	return r.public.Load() && r.filesSocket.Load() && r.mcpSocket.Load() && !r.draining.Load()
}

// NewHealth serves the probe endpoints on the health port (08 §2), which is
// not in the Service and not routed. GET /healthz is liveness and always 200;
// GET /readyz is 200 once ready() reports true and 503 before.
func NewHealth(ready func() bool) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeStatus(w, http.StatusOK)
	})
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, _ *http.Request) {
		if !ready() {
			writeStatus(w, http.StatusServiceUnavailable)
			return
		}
		writeStatus(w, http.StatusOK)
	})
	return mux
}

func writeStatus(w http.ResponseWriter, code int) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(code)
	_, _ = w.Write([]byte(http.StatusText(code) + "\n"))
}
