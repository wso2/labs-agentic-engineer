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
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/wso2/aep/aep-api/internal/gen"
	"github.com/wso2/aep/aep-api/internal/platform/apierr"
)

// The flat error envelope every non-2xx response carries (contract schema
// Error): {code, message, details?}. code is a stable machine-readable slug;
// details appears only on field-level validation errors. This replaced the
// RFC 9457 problem-details dialect at the contract-first cutover.
//
// The envelope itself now lives in platform/apierr, because a domain SLICE must
// be able to return a 404 without importing the edge (which would be a cycle —
// the edge composes slices). What stays here is the writers: the one place that
// turns an *apierr.Error into wire bytes. The aliases below are the edge's stable
// error-code/constructor surface, shared by the S2S internalServer + the validator.
const (
	CodeValidationFailed   = apierr.CodeValidationFailed
	CodeBadRequest         = apierr.CodeBadRequest
	CodeUnauthorized       = apierr.CodeUnauthorized
	CodeForbidden          = apierr.CodeForbidden
	CodeNotFound           = apierr.CodeNotFound
	CodeConflict           = apierr.CodeConflict
	CodeInternal           = apierr.CodeInternal
	CodeBadGateway         = apierr.CodeBadGateway
	CodeServiceUnavailable = apierr.CodeServiceUnavailable
)

// apiError is the transport error the strict handlers and middleware return.
// An ALIAS, not a new type: the writers must recognise the errors a domain slice
// constructs via platform/apierr and the ones legacy handlers construct here as
// the same thing, or one of the two would fall through to a bare 500.
type apiError = apierr.Error

func errUnauthorized(msg string) error       { return apierr.Unauthorized(msg) }
func errForbidden(msg string) error          { return apierr.Forbidden(msg) }
func errNotFound(msg string) error           { return apierr.NotFound(msg) }
func errInternal(msg string) error           { return apierr.Internal(msg) }
func errServiceUnavailable(msg string) error { return apierr.ServiceUnavailable(msg) }

// writeErrorEnvelope writes the flat envelope. It is the single place a
// non-2xx body is produced on the public edge.
func writeErrorEnvelope(w http.ResponseWriter, status int, code, msg string, details []gen.ErrorDetail) {
	body := gen.Error{Code: code, Message: msg, Details: details}
	writeJSON(w, status, body)
}

// writeResponseError is the strict handler's ResponseErrorHandlerFunc: a typed
// *apiError writes its own status/code; anything else is an unclassified
// failure and becomes an opaque 500 (never leaking the internal cause).
//
// Before either, an AE Studio answer anywhere in the chain speaks for itself
// (classifyAEStudio) — unless a slice already gave the request a 4xx verdict
// of its own, which stands. A slice's 5xx fallback carries its cause
// (apierr.WithCause), so "internal error" never hides a 503 or a 409.
func writeResponseError(w http.ResponseWriter, r *http.Request, err error) {
	var ae *apiError
	typed := errors.As(err, &ae)
	if !typed || ae.Status >= http.StatusInternalServerError {
		if c := classifyAEStudio(err); c != nil {
			slog.WarnContext(r.Context(), "ae studio answer", "path", r.URL.Path, "status", c.Status, "code", c.Code)
			writeAPIError(w, c)
			return
		}
	}
	if typed {
		writeAPIError(w, ae)
		return
	}
	slog.ErrorContext(r.Context(), "unclassified handler error", "path", r.URL.Path, "err", err)
	writeErrorEnvelope(w, http.StatusInternalServerError, CodeInternal, "internal error", nil)
}

// writeAPIError writes a typed error, with its Retry-After when it has one.
func writeAPIError(w http.ResponseWriter, ae *apiError) {
	if ae.RetryAfter > 0 {
		w.Header().Set("Retry-After", strconv.Itoa(ae.RetryAfter))
	}
	writeErrorEnvelope(w, ae.Status, ae.Code, ae.Message, ae.Details)
}

// writeRequestError is the strict handler's RequestErrorHandlerFunc and the
// generated router's ErrorHandlerFunc: request-shape problems the generated
// code detects before the handler runs (undecodable JSON body, unparsable
// path/query params) — always a 400 in the new dialect.
func writeRequestError(w http.ResponseWriter, _ *http.Request, err error) {
	writeErrorEnvelope(w, http.StatusBadRequest, CodeValidationFailed, err.Error(), nil)
}
