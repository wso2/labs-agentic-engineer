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


// Package problem writes ae-studio-tools' error body, application/problem+json
// {type, title, status, detail, code}. It is a leaf so that both the auth gates
// and the edge routes can answer with it without an import cycle.
package problem

import (
	"encoding/json"
	"net/http"
)

// Write sends one problem response. detail is a fixed, value-free sentence:
// callers never put a token, a claim value or a secret in it.
func Write(w http.ResponseWriter, status int, code, detail string) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"type": "about:blank", "title": http.StatusText(status), "status": status, "detail": detail, "code": code,
	})
}
