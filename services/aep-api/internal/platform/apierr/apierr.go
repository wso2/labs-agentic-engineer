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

// Package apierr is the transport error every non-2xx response is built from:
// the flat envelope {code, message, details?} of the contract's Error schema.
//
// It lives in the kernel because a slice must be able to say "404" without
// importing the edge. The edge composes slices (edge -> domain/httpapi ->
// slice), so a slice that imported the edge for its error constructors would be
// an import cycle; and the alternative — slices returning domain sentinels for
// the edge to translate — would force the edge to know every domain's error
// vocabulary, which is precisely the centralisation the slice layout removes.
//
// The split is: constructors HERE (used by slices), writers in the edge (which
// turns an *Error into the wire envelope). Nothing here writes to a
// ResponseWriter.
package apierr

import (
	"net/http"

	"github.com/wso2/aep/aep-api/internal/gen"
)

// The stable machine-readable slugs of the envelope's `code`. They are part of
// the contract: clients branch on them, so they are renamed only with the
// contract.
const (
	CodeValidationFailed   = "validation_failed"
	CodeBadRequest         = "bad_request"
	CodeUnauthorized       = "unauthorized"
	CodeForbidden          = "forbidden"
	CodeNotFound           = "not_found"
	CodeConflict           = "conflict"
	CodePaymentRequired    = "payment_required"
	CodeInternal           = "internal_error"
	CodeBadGateway         = "bad_gateway"
	CodeServiceUnavailable = "service_unavailable"
)

// Error is the transport error strict handlers and middleware return; the edge's
// central writers turn it into the envelope. Details appears only on
// field-level validation errors.
type Error struct {
	Status  int
	Code    string
	Message string
	Details []gen.ErrorDetail
}

func (e *Error) Error() string { return e.Message }

// New builds an Error with an explicit status/code — for the handful of cases
// outside the constructors below (413, and codes a slice owns).
func New(status int, code, msg string, details []gen.ErrorDetail) error {
	return &Error{Status: status, Code: code, Message: msg, Details: details}
}

func BadRequest(msg string) error {
	return &Error{Status: http.StatusBadRequest, Code: CodeBadRequest, Message: msg}
}

func Unauthorized(msg string) error {
	return &Error{Status: http.StatusUnauthorized, Code: CodeUnauthorized, Message: msg}
}

func Forbidden(msg string) error {
	return &Error{Status: http.StatusForbidden, Code: CodeForbidden, Message: msg}
}

func NotFound(msg string) error {
	return &Error{Status: http.StatusNotFound, Code: CodeNotFound, Message: msg}
}

func Conflict(msg string) error {
	return &Error{Status: http.StatusConflict, Code: CodeConflict, Message: msg}
}

// PaymentRequired is HTTP 402 — entitlement/subscription gate (project quota,
// inactive subscription, agent concurrency). The message is meant for the
// console: pass through the platform's sentence when we have one.
func PaymentRequired(msg string) error {
	return &Error{Status: http.StatusPaymentRequired, Code: CodePaymentRequired, Message: msg}
}

func Internal(msg string) error {
	return &Error{Status: http.StatusInternalServerError, Code: CodeInternal, Message: msg}
}

func BadGateway(msg string) error {
	return &Error{Status: http.StatusBadGateway, Code: CodeBadGateway, Message: msg}
}

func ServiceUnavailable(msg string) error {
	return &Error{Status: http.StatusServiceUnavailable, Code: CodeServiceUnavailable, Message: msg}
}
