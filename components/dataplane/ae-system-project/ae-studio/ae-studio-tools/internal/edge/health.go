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

import "net/http"

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
