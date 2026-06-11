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

	"github.com/wso2/asdlc/database-service/controllers"
	"github.com/wso2/asdlc/database-service/middleware"
	"github.com/wso2/asdlc/database-service/middleware/logger"
)

// AppParams holds all dependencies needed to build the HTTP handler.
type AppParams struct {
	DatabaseCtrl controllers.DatabaseController
}

// NewHandler assembles the full HTTP handler with middleware and routes.
func NewHandler(params AppParams) http.Handler {
	mux := http.NewServeMux()

	// Health check
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`)) //nolint:errcheck
	})

	// API routes (no JWT — internal service)
	registerDatabaseRoutes(mux, params.DatabaseCtrl)

	// Global middleware stack (outermost applied last)
	var handler http.Handler = mux
	handler = logger.RequestLogger()(handler)
	handler = middleware.RecovererOnPanic()(handler)

	return handler
}
