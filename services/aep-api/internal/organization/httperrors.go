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

package organization

import (
	"errors"
	"net/http"

	"github.com/wso2/aep/aep-api/internal/gen"
	"github.com/wso2/aep/aep-api/internal/platform/apierr"
)

// This file holds the HTTP vocabulary the /config slices share. A slice cannot
// import a sibling slice (slice ⊥ sibling), so the section-error map the save
// and Test connection both answer with lives in the domain ROOT they import.

// MapConfigError turns a /config failure into the envelope. A SectionError
// carries the offending section, so the response includes a body.<section>
// field the console uses to highlight that form section, and the refusal's own
// code when it has one (llm_key_rejected, agents_subscription_requires_claude_code,
// …), so a client can branch on the reason without parsing prose. Anything
// else collapses to an opaque 500 that never echoes the internal cause.
func MapConfigError(err error) error {
	var se *SectionError
	if !errors.As(err, &se) {
		return apierr.Internal("internal error")
	}
	details := []gen.ErrorDetail{{Field: "body." + se.Section, Message: se.Message}}
	switch se.Status {
	case http.StatusConflict:
		return apierr.New(http.StatusConflict, apierr.CodeConflict, se.Message, details)
	case http.StatusBadGateway:
		return apierr.New(http.StatusBadGateway, codeOr(se.Code, apierr.CodeBadGateway), se.Message, details)
	case http.StatusServiceUnavailable:
		return apierr.New(http.StatusServiceUnavailable, codeOr(se.Code, apierr.CodeServiceUnavailable), se.Message, details)
	default:
		return apierr.New(http.StatusBadRequest, codeOr(se.Code, apierr.CodeValidationFailed), se.Message, details)
	}
}

func codeOr(code, fallback string) string {
	if code != "" {
		return code
	}
	return fallback
}
