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
	"path"
	"strings"

	"github.com/wso2/aep/aep-api/internal/clients/aestudiotools"
	"github.com/wso2/aep/aep-api/internal/gen"
	"github.com/wso2/aep/aep-api/internal/platform/apierr"
	"github.com/wso2/aep/aep-api/internal/platform/officetext"
	"github.com/wso2/aep/aep-api/internal/platform/tenant"
	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
)

// referencesField is the multipart field the console repeats once per document.
const referencesField = "files"

// PutProjectReferences passes the reference documents attached on the create
// view through to the org's AE Studio pod, which stores them. They
// are transient turn inputs, never committed (console ADR-0017); the pod
// validates them (count, size, type) and replaces the whole set, so a retry
// after a partial failure converges rather than accumulating.
//
// The strict server hands this handler a *multipart.Reader, so the bytes
// cannot pass through as they came: each `files` part is re-streamed, chunk by
// chunk, into a new multipart body under the same field and file name.
// No part is buffered whole except an Office document, which is converted to
// markdown first (copyOfficePart). A body that breaks off mid-part, or an
// Office part that cannot be converted, aborts the pod's upload with that
// error, so the pod never stores a truncated or partial set.
func (h *Handler) PutProjectReferences(ctx context.Context, request gen.PutProjectReferencesRequestObject) (gen.PutProjectReferencesResponseObject, error) {
	org := tenant.BoundOrgFromContext(ctx)
	if request.Body == nil {
		return nil, apierr.BadRequest("missing '" + referencesField + "' field")
	}
	ref, _, err := sourcecontrol.RepoRefFor(ctx, h.repos, org, request.ProjectName)
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
	copyErr := <-copied
	var refused refusedPartError
	switch {
	case errors.As(copyErr, &refused):
		return nil, apierr.BadRequest(string(refused))
	case copyErr != nil && !errors.Is(copyErr, io.ErrClosedPipe):
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
		if ext := strings.ToLower(path.Ext(part.FileName())); officetext.Extensions[ext] {
			if err := copyOfficePart(part, ext, out); err != nil {
				return err
			}
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

// copyOfficePart writes an Office part to out as the markdown it converts to
// (#878 S5), named `<name>.md`: the models do not read Office formats and the
// pod does not store them, and the name keeps the original's (Policy.docx.md),
// which is what the agent cites it by. It is the one part aep-api holds whole,
// and only up to the pod's per-document limit, read one byte past it because
// io.LimitReader ends a capped read with io.EOF and a truncated document would
// convert to the wrong words. A part over the limit, one whose markdown would
// be, or one that does not convert is a refusedPartError, returned before any
// byte of the part is written.
func copyOfficePart(part *multipart.Part, ext string, out *multipart.Writer) error {
	name := part.FileName()
	content, err := io.ReadAll(io.LimitReader(part, sourcecontrol.MaxReferenceBytes+1))
	if err != nil {
		return err
	}
	tooLarge := refusedPartError(fmt.Sprintf("%q exceeds the %d MiB per-document limit", name, sourcecontrol.MaxReferenceBytes>>20))
	if len(content) > sourcecontrol.MaxReferenceBytes {
		return tooLarge
	}
	// The markdown is what the pod stores, so it is held to the same limit,
	// while it is built: a small zip can expand into far more text.
	text, err := convertOffice(ext, content)
	switch {
	case errors.Is(err, officetext.ErrTooLarge):
		return tooLarge
	case err != nil:
		return refusedPartError(fmt.Sprintf("%q could not be read as a %s file", name, ext))
	}
	header := textproto.MIMEHeader{}
	header.Set("Content-Disposition", multipart.FileContentDisposition(referencesField, name+".md"))
	header.Set("Content-Type", "text/markdown; charset=utf-8")
	w, err := out.CreatePart(header)
	if err != nil {
		return err
	}
	_, err = io.WriteString(w, text)
	return err
}

// officeMarkdown is the Office converter; a variable so a test can make it
// panic.
var officeMarkdown = officetext.Markdown

// errConversionPanicked is a document the converter panicked on.
var errConversionPanicked = errors.New("office conversion panicked")

// convertOffice converts an Office document to markdown within the pod's
// per-document limit. It runs on the copy goroutine, where net/http recovers
// nothing, so a panic on a crafted document would end the process: it is
// recovered here as errConversionPanicked (the "could not be read" 400). The
// log names the panic's class only, never its value, which can carry the
// document's text.
func convertOffice(ext string, content []byte) (text string, err error) {
	defer func() {
		if r := recover(); r != nil {
			slog.Warn("references.office_conversion_panicked", "ext", ext, "class", fmt.Sprintf("%T", r))
			text, err = "", errConversionPanicked
		}
	}()
	return officeMarkdown(ext, content, sourcecontrol.MaxReferenceBytes)
}

// refusedPartError is a part aep-api itself refuses (an Office document it
// cannot convert), worded as the caller's 400 message.
type refusedPartError string

func (e refusedPartError) Error() string { return string(e) }

// codeRequestTooLarge is the envelope code of an upload the pod refused for size.
const codeRequestTooLarge = "request_too_large"

// mapReferenceError maps what the pod (or the project lookup) answered onto
// the envelope. The AE Studio answers (absent, unavailable, misconfigured,
// owner not allowed) are not mapped here: they stay in the chain and the
// edge's one classifier speaks for them, Retry-After included.
func mapReferenceError(ctx context.Context, err error) error {
	var se *aestudiotools.StatusError
	switch {
	case errors.Is(err, sourcecontrol.ErrRepoNotFound):
		return apierr.NotFound("project repository not found")
	case errors.Is(err, sourcecontrol.ErrReferenceRejected):
		return apierr.BadRequest(strings.TrimPrefix(err.Error(), sourcecontrol.ErrReferenceRejected.Error()+": "))
	case errors.As(err, &se) && se.Status == http.StatusRequestEntityTooLarge:
		return apierr.New(http.StatusRequestEntityTooLarge, codeRequestTooLarge, "the reference documents are too large", nil)
	case errors.As(err, &se):
		slog.WarnContext(ctx, "references: AE Studio refused the upload", "status", se.Status, "code", se.Code)
		return apierr.WithCause(apierr.BadGateway(fmt.Sprintf("AE Studio refused the upload (%d)", se.Status)), err)
	default:
		slog.ErrorContext(ctx, "references: upload failed", "error", err)
		return apierr.WithCause(apierr.Internal("internal error"), err)
	}
}
