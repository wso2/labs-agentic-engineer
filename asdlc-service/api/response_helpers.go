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
	"errors"
	"fmt"
	"net/http"

	"github.com/wso2/asdlc/asdlc-service/internal/feature/orgcreds"
)

// ----------------------------------------------------------------------------
// Response helpers shared by the org-scoped credential routes.
// ----------------------------------------------------------------------------

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeJSONError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func writeServiceError(w http.ResponseWriter, err error) {
	var ve *orgcreds.ValidationError
	var ce *orgcreds.ConflictError
	var ne *orgcreds.NotFoundError
	switch {
	case errors.As(err, &ve):
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": ve.Message, "code": ve.Code})
	case errors.As(err, &ce):
		writeJSON(w, http.StatusConflict, map[string]string{"error": ce.Reason})
	case errors.As(err, &ne):
		writeJSON(w, http.StatusNotFound, map[string]string{"error": ne.Error()})
	default:
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("internal: %v", err)})
	}
}
