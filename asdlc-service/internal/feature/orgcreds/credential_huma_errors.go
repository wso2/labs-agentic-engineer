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

// credential_huma_errors.go — Huma-side error mapping + small marshalling
// helpers shared by the orggithub_huma.go and organthropic_huma.go register
// functions. mapCredentialError is the RFC 9457 analogue of
// writeCredentialServiceError (credential_errors.go): it preserves the exact
// status classification the legacy controllers applied.

package orgcreds

import (
	"encoding/json"
	"errors"

	"github.com/danielgtaylor/huma/v2"
)

// mapCredentialError translates the typed errors returned by
// CredentialService / AnthropicCredentialService into RFC 9457 problem
// responses, mirroring writeCredentialServiceError: 404 for NotFoundError,
// 409 for ConflictError, 400 for ValidationError, 503 for
// ErrAppBindNotConfigured, 500 otherwise. The error message is preserved so
// the console can read the structured cause where present.
func mapCredentialError(err error) error {
	var nfe *NotFoundError
	if errors.As(err, &nfe) {
		return huma.Error404NotFound(err.Error())
	}
	var ce *ConflictError
	if errors.As(err, &ce) {
		return huma.Error409Conflict(err.Error())
	}
	var ve *ValidationError
	if errors.As(err, &ve) {
		return huma.Error400BadRequest(err.Error())
	}
	if errors.Is(err, ErrAppBindNotConfigured) {
		return huma.Error503ServiceUnavailable(err.Error())
	}
	return huma.Error500InternalServerError(err.Error())
}

// structToMap round-trips a value through JSON into a flat map so a typed
// projection can be returned through a map-bodied output that ALSO carries a
// differently-shaped "not connected" payload — without changing the on-wire
// JSON shape of either branch (legacy parity).
func structToMap(v any) (map[string]any, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, err
	}
	return m, nil
}
