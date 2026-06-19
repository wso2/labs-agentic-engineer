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

// Package httpkit holds the shared HTTP response writers (the eventual home of
// WriteJSON/WriteAppError and the SSE writer, §4.0). For the gate phase it
// exposes the Write40x helpers the central tenant gate uses. Bodies are kept
// byte-identical to the legacy utils.WriteErrorResponse payload by delegating
// to it, so no client/test sees a changed error shape during the migration.
package httpkit

import (
	"net/http"

	"github.com/wso2/asdlc/asdlc-service/utils"
)

// Write400 writes a 400 Bad Request with the given client-facing message.
func Write400(w http.ResponseWriter, msg string) {
	utils.WriteErrorResponse(w, http.StatusBadRequest, msg)
}

// Write401 writes a 401 Unauthorized.
func Write401(w http.ResponseWriter) {
	utils.WriteErrorResponse(w, http.StatusUnauthorized, "authentication required")
}

// Write404 writes a 404 Not Found. The gate uses the SAME body for wrong-org
// and no-such-org so cross-org existence is never leaked (§6.1a).
func Write404(w http.ResponseWriter, msg string) {
	utils.WriteErrorResponse(w, http.StatusNotFound, msg)
}

// Write500 writes a 500 Internal Server Error with a generic message.
func Write500(w http.ResponseWriter) {
	utils.WriteErrorResponse(w, http.StatusInternalServerError, "internal error")
}
