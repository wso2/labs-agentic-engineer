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

package projects

import (
	"errors"
	"net/http"
	"strings"

	"github.com/wso2/aep/aep-api/internal/clients/openchoreo"
	"github.com/wso2/aep/aep-api/internal/organization"
	"github.com/wso2/aep/aep-api/internal/platform/apierr"
	"github.com/wso2/aep/aep-api/internal/platform/ocerr"
	"github.com/wso2/aep/aep-api/internal/platform/validate"
	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
)

// This file holds the shared HTTP vocabulary the projects slices lean on. A slice
// cannot import a sibling slice (slice ⊥ sibling), so the behaviour two slices
// share — slug guards and the sentinel→envelope error maps — lives in the domain
// ROOT they both import.

// RequireSlug validates a single DNS-label slug path param, returning a 400
// envelope error on failure. Delegates to validate.Slug.
func RequireSlug(name, v string) error {
	if err := validate.Slug(v); err != nil {
		return apierr.BadRequest(name + ": " + err.Error())
	}
	return nil
}

// RequireComponentSlugs validates the projectName + componentName path params
// as DNS-label slugs.
func RequireComponentSlugs(projectName, componentName string) error {
	if err := RequireSlug("projectName", projectName); err != nil {
		return err
	}
	return RequireSlug("componentName", componentName)
}

// codeNoWriteTarget is the envelope code for a write refused because the
// project's deployment pipeline names no write target (ADR-0039).
const codeNoWriteTarget = "no_write_target"

// codeProjectTypeNotFound is the envelope code for a create refused because
// OpenChoreo cannot find the org's ProjectType for the new project.
const codeProjectTypeNotFound = "project_type_not_found"

// MapProjectError translates project + OpenChoreo sentinel errors into the
// envelope. The feature sentinels (translated from OC by the service's
// translateHTTPError) carry the fixed user-facing messages; any remaining raw
// OC sentinel rides the shared ocerr classifier.
func MapProjectError(err error) error {
	var nwt *openchoreo.ErrNoWriteTarget
	switch {
	case errors.Is(err, ErrUnauthorized) || errors.Is(err, openchoreo.ErrUnauthorized):
		return apierr.Unauthorized("invalid or expired token")
	case errors.Is(err, ErrProjectNotFound):
		return apierr.NotFound("project not found")
	case errors.Is(err, ErrForbidden):
		return apierr.Forbidden("insufficient permissions to perform this action")
	case sourcecontrol.IsRepoNameConflict(err):
		return apierr.Conflict("a repository with this name already exists — choose another repository name")
	case errors.Is(err, sourcecontrol.ErrRepoDeletePending):
		return apierr.Conflict("an earlier delete of this project did not finish — delete the project again, then create it")
	case errors.As(err, &nwt):
		// The project's pipeline names no write target: the caller's
		// configuration to fix, so the resolver's words go back verbatim.
		return apierr.New(http.StatusUnprocessableEntity, codeNoWriteTarget, nwt.Error(), nil)
	case errors.Is(err, ErrProjectTypeNotFound):
		// The org's platform did not seed the ProjectType every project
		// references: configuration to fix, in OpenChoreo's words.
		return apierr.New(http.StatusUnprocessableEntity, codeProjectTypeNotFound, err.Error(), nil)
	case errors.Is(err, openchoreo.ErrPaymentRequired):
		// Prefer the platform sentence (quota / inactive subscription) over the
		// bare sentinel — the console already renders Error.message in an Alert.
		return apierr.PaymentRequired(paymentRequiredMessage(err))
	}
	if status, ok := ocerr.Status(err); ok {
		return errFromStatus(status, err.Error())
	}
	return apierr.WithCause(apierr.Internal("internal error"), err)
}

// paymentRequiredMessage strips the "payment required: " sentinel prefix so the
// console Alert shows the platform sentence alone.
func paymentRequiredMessage(err error) string {
	const fallback = "Quota limit reached. Upgrade your subscription to continue."
	if err == nil {
		return fallback
	}
	msg := err.Error()
	prefix := openchoreo.ErrPaymentRequired.Error() + ": "
	if strings.HasPrefix(msg, prefix) {
		if trimmed := strings.TrimSpace(strings.TrimPrefix(msg, prefix)); trimmed != "" {
			return trimmed
		}
	}
	if msg == openchoreo.ErrPaymentRequired.Error() {
		return fallback
	}
	return msg
}

// MapComponentError translates an OpenChoreo sentinel that reached
// componentService into its envelope status via the shared ocerr classifier
// (401/403/404/409/400/500, matching project and organization). An error that
// is not an OC sentinel collapses to a fixed-message 500 that never echoes the
// internal cause. Handler-specific branches (409 not-service, 503
// logs-unavailable, 404 openapi-not-found) are handled at the call site before
// delegating here.
func MapComponentError(err error, internalMsg string) error {
	if errors.Is(err, organization.ErrOrgDisconnected) {
		// The org has no github-pat reference (never connected, or
		// disconnected): a state the caller fixes by connecting GitHub.
		return apierr.Conflict("GitHub is not connected for this organization; connect GitHub before triggering a build")
	}
	if status, ok := ocerr.Status(err); ok {
		return errFromStatus(status, err.Error())
	}
	return apierr.WithCause(apierr.Internal(internalMsg), err)
}

// errFromStatus maps a sentinel-classified HTTP status (e.g. an OpenChoreo
// pass-through classified by ocerr.Status) onto the envelope, mirroring the
// retired humakit.ErrorFromStatus ladder.
func errFromStatus(status int, msg string) error {
	switch status {
	case http.StatusBadRequest:
		return apierr.BadRequest(msg)
	case http.StatusUnauthorized:
		return apierr.Unauthorized(msg)
	case http.StatusForbidden:
		return apierr.Forbidden(msg)
	case http.StatusNotFound:
		return apierr.NotFound(msg)
	case http.StatusConflict:
		return apierr.Conflict(msg)
	case http.StatusPaymentRequired:
		return apierr.PaymentRequired(msg)
	default:
		return apierr.Internal(msg)
	}
}
