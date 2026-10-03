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

package files

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"strings"

	"github.com/wso2/aep/aep-api/internal/clients/aestudiotools"
	"github.com/wso2/aep/aep-api/internal/gen"
	"github.com/wso2/aep/aep-api/internal/platform/apierr"
	"github.com/wso2/aep/aep-api/internal/platform/tenant"
	"github.com/wso2/aep/aep-api/internal/spec"
)

// referencesField is the multipart field the console repeats once per document.
const referencesField = "files"

// PutProjectReferences passes the reference documents attached on the create
// view through to the org's AE Studio pod, which stores them (09 §1). They
// are transient turn inputs, never committed (console ADR-0017); the pod
// validates them (count, size, type) and replaces the whole set, so a retry
// after a partial failure converges rather than accumulating.
//
// The strict server hands this handler a *multipart.Reader, so the bytes
// cannot pass through as they came: each `files` part is re-streamed, chunk by
// chunk, into a new multipart body under the same field and file name (R21).
// No part is buffered whole. A body that breaks off mid-part aborts the pod's
// upload with the same error, so the pod never stores a truncated set.
func (h *Handler) PutProjectReferences(ctx context.Context, request gen.PutProjectReferencesRequestObject) (gen.PutProjectReferencesResponseObject, error) {
	org := tenant.BoundOrgFromContext(ctx)
	if request.Body == nil {
		return nil, apierr.BadRequest("missing '" + referencesField + "' field")
	}
	ref, _, err := spec.RepoRefFor(ctx, h.repos, org, request.ProjectName)
	if err != nil {
		return nil, mapReferenceError(ctx, err)
	}

	pr, pw := io.Pipe()
	mw := multipart.NewWriter(pw)
	copied := make(chan error, 1)
	go func() {
		err := copyReferenceParts(request.Body, mw)
		_ = pw.CloseWithError(err) // nil closes the body cleanly
		copied <- err
	}()
	// PutReferences closes pr on return, which unblocks the copy if the pod
	// answered before reading everything.
	putErr := h.refs.PutReferences(ctx, ref, mw.FormDataContentType(), pr)
	// Wait for the copy: it reads the request body, which must not be read
	// after this handler returns.
	if copyErr := <-copied; copyErr != nil && !errors.Is(copyErr, io.ErrClosedPipe) {
		return nil, apierr.BadRequest("can't decode multipart body: " + copyErr.Error())
	}
	if putErr != nil {
		return nil, mapReferenceError(ctx, putErr)
	}

	// Release the kickoff a create with `referencesPending` held (#562): the
	// documents are now in front of the agent, so the interview can start.
	// Idempotent (the ledger guard and the deterministic turn id), which is
	// what makes a re-upload safe.
	if h.kickoff != nil {
		h.kickoff.Kickoff(ctx, org, request.ProjectName)
	}
	return gen.PutProjectReferences204Response{}, nil
}

// copyReferenceParts copies every `files` part of in into out, keeping each
// part's file name and content type, then closes out. Other fields are
// skipped, as the pod accepts only `files`.
func copyReferenceParts(in *multipart.Reader, out *multipart.Writer) error {
	for {
		part, err := in.NextPart()
		if errors.Is(err, io.EOF) {
			return out.Close()
		}
		if err != nil {
			return err
		}
		if part.FormName() != referencesField {
			continue
		}
		header := textproto.MIMEHeader{}
		header.Set("Content-Disposition", multipart.FileContentDisposition(referencesField, part.FileName()))
		if ct := part.Header.Get("Content-Type"); ct != "" {
			header.Set("Content-Type", ct)
		}
		w, err := out.CreatePart(header)
		if err != nil {
			return err
		}
		if _, err := io.Copy(w, part); err != nil {
			return err
		}
	}
}

// Problem codes of the pod's refusals.
const (
	codeAEStudioMisconfigured = "ae_studio_misconfigured"
	codeAEStudioUnavailable   = "ae_studio_unavailable"
	codeGitHubNotConnected    = "github_not_connected"
	codeRequestTooLarge       = "request_too_large"
)

// mapReferenceError maps what the pod (or the project lookup) answered onto
// the envelope. A misconfigured AE-only client is an operator fault: 503
// with its own code and no Retry-After (C3).
func mapReferenceError(ctx context.Context, err error) error {
	var se *aestudiotools.StatusError
	switch {
	case errors.Is(err, spec.ErrProjectRepoNotFound):
		return apierr.NotFound("project repository not found")
	case errors.Is(err, aestudiotools.ErrReferenceRejected):
		return apierr.BadRequest(strings.TrimPrefix(err.Error(), aestudiotools.ErrReferenceRejected.Error()+": "))
	case errors.Is(err, aestudiotools.ErrAEStudioMisconfigured):
		return apierr.New(http.StatusServiceUnavailable, codeAEStudioMisconfigured,
			"AE Studio is not configured on this platform — contact your platform admin", nil)
	case errors.Is(err, aestudiotools.ErrAEStudioUnavailable):
		return apierr.New(http.StatusServiceUnavailable, codeAEStudioUnavailable,
			"AE Studio is not ready — try again in a few seconds", nil)
	case errors.Is(err, aestudiotools.ErrAEStudioAbsent):
		return apierr.New(http.StatusConflict, codeGitHubNotConnected, "connect GitHub to continue", nil)
	case errors.As(err, &se) && se.Status == http.StatusRequestEntityTooLarge:
		return apierr.New(http.StatusRequestEntityTooLarge, codeRequestTooLarge, "the reference documents are too large", nil)
	case errors.As(err, &se):
		slog.WarnContext(ctx, "references: AE Studio refused the upload", "status", se.Status, "code", se.Code)
		return apierr.BadGateway(fmt.Sprintf("AE Studio refused the upload (%d)", se.Status))
	default:
		slog.ErrorContext(ctx, "references: upload failed", "error", err)
		return apierr.Internal("internal error")
	}
}
