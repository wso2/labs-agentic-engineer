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

// Package ocerr is the single home for translating an OpenChoreo sentinel
// error (openchoreo.Err*) into the HTTP status the BFF surfaces for it. The
// project, component, and organization features each used to re-derive this
// classification inline; consolidating it here keeps the OC-status contract
// in one place. It lives under platform/ so the transport layer keeps its
// lean auth/tenant/huma dependency set — this package is the only platform
// leaf that imports the OpenChoreo client, which is allowed because a client
// is not a feature (see internal/arch feature-free invariant).
//
// Callers pair Status with the api layer's errFromStatus to build the
// problem response; features that surface a coarser contract (organization
// only distinguishes 401) branch on the returned status directly.
package ocerr

import (
	"errors"
	"net/http"

	"github.com/wso2/aep/aep-api/internal/clients/openchoreo"
)

// Status maps an OpenChoreo sentinel error to its HTTP status. ok is false
// when err is not a recognized OC sentinel, so callers fall back to their own
// default (usually an opaque 500). A given error wraps exactly one sentinel,
// so the switch order is not load-bearing.
func Status(err error) (status int, ok bool) {
	switch {
	case errors.Is(err, openchoreo.ErrBadRequest):
		return http.StatusBadRequest, true
	case errors.Is(err, openchoreo.ErrUnauthorized):
		return http.StatusUnauthorized, true
	case errors.Is(err, openchoreo.ErrForbidden):
		return http.StatusForbidden, true
	case errors.Is(err, openchoreo.ErrNotFound):
		return http.StatusNotFound, true
	case errors.Is(err, openchoreo.ErrConflict):
		return http.StatusConflict, true
	case errors.Is(err, openchoreo.ErrPaymentRequired):
		return http.StatusPaymentRequired, true
	case errors.Is(err, openchoreo.ErrInternalServerError):
		return http.StatusInternalServerError, true
	}
	return 0, false
}
