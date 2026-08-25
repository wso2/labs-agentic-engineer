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
	"mime/multipart"
	"path"
	"strings"

	"github.com/wso2/aep/aep-api/internal/gen"
	"github.com/wso2/aep/aep-api/internal/platform/apierr"
	"github.com/wso2/aep/aep-api/internal/platform/gitfs"
	"github.com/wso2/aep/aep-api/internal/platform/tenant"
)

// referencesField is the multipart field the console repeats once per document.
const referencesField = "files"

// PutProjectReferences stores the reference documents attached on the create
// view. They are transient turn inputs, never committed (console ADR-0017), so
// this deliberately does NOT go through the Files API's apply: there is no
// commit, no baseSha precondition, and no specs/ path scope to honour — the
// engine owns the store and overlays it into each turn's snapshot.
//
// The upload replaces the whole set, so a retry after a partial failure
// converges rather than accumulating.
func (h *Handler) PutProjectReferences(ctx context.Context, request gen.PutProjectReferencesRequestObject) (gen.PutProjectReferencesResponseObject, error) {
	org := tenant.BoundOrgFromContext(ctx)
	docs, err := readReferenceParts(request.Body)
	if err != nil {
		return nil, err
	}
	if err := h.files.PutReferences(ctx, org, request.ProjectName, docs); err != nil {
		return nil, mapReferenceError(err)
	}
	// Release the kickoff a create with `referencesPending` held (#562): the
	// documents are now in front of the agent, so the interview can start.
	// Idempotent on "has this project ever run a turn", which is what makes a
	// re-upload safe — it replaces the stored set without starting a second
	// interview over the first.
	if h.kickoff != nil {
		h.kickoff.Kickoff(ctx, org, request.ProjectName)
	}
	return gen.PutProjectReferences204Response{}, nil
}

// readReferenceParts drains the multipart body into memory. Bounded twice: the
// engine's per-document cap is enforced on the bytes read here — with one byte
// past it read deliberately, because io.LimitReader ends a capped read with
// io.EOF, which is indistinguishable from a small file ending, and silently
// storing a truncated PDF is worse than refusing the upload.
func readReferenceParts(body *multipart.Reader) ([]gitfs.ReferenceDoc, error) {
	if body == nil {
		return nil, apierr.BadRequest("missing '" + referencesField + "' field")
	}
	var docs []gitfs.ReferenceDoc
	byStoredName := map[string]string{}
	for {
		part, err := body.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, apierr.BadRequest("can't decode multipart body: " + err.Error())
		}
		if part.FormName() != referencesField {
			continue
		}
		// Refuse early rather than buffering an eleventh document only for the
		// engine to reject the set.
		if len(docs) >= gitfs.MaxReferenceCount {
			return nil, apierr.BadRequest(fmt.Sprintf(
				"at most %d reference documents per project", gitfs.MaxReferenceCount))
		}
		content, err := io.ReadAll(io.LimitReader(part, gitfs.MaxReferenceBytes+1))
		if err != nil {
			return nil, apierr.BadRequest("read upload: " + err.Error())
		}
		// Sanitizing can map two DIFFERENT uploads onto one stored name
		// ("My Notes.md" and "my-notes.md"). The engine rejects the batch for
		// the duplicate, so surface the collision here where both original
		// names are still known — "duplicate name" alone leaves the caller
		// guessing which two of their files collided. The console screens for
		// this before uploading; this is the API's own guard.
		name := sanitizeReferenceName(part.FileName())
		if prior, ok := byStoredName[name]; ok {
			return nil, apierr.BadRequest(fmt.Sprintf(
				"%q and %q both become %q — rename one", prior, part.FileName(), name))
		}
		byStoredName[name] = part.FileName()
		if len(content) > gitfs.MaxReferenceBytes {
			return nil, apierr.BadRequest(fmt.Sprintf(
				"%q exceeds the %d MiB per-document limit", name, gitfs.MaxReferenceBytes>>20))
		}
		docs = append(docs, gitfs.ReferenceDoc{Name: name, Content: content})
	}
	if len(docs) == 0 {
		return nil, apierr.BadRequest("no reference documents in the request")
	}
	return docs, nil
}

// sanitizeReferenceName reduces a browser-supplied file name to the bare,
// store-safe name. The client sends a plain name today, but the field is
// attacker-controlled: path.Base strips any directory a crafted part carries
// (including a Windows-style one, which path.Base would otherwise keep whole),
// and the stem loses everything outside the engine's allowed alphabet. The
// engine validates the result again and is the authority — this only stops a
// recoverable name from being refused for punctuation the user never typed.
func sanitizeReferenceName(raw string) string {
	name := path.Base(strings.ReplaceAll(strings.TrimSpace(raw), `\`, "/"))
	ext := strings.ToLower(path.Ext(name))
	stem := strings.TrimSuffix(name, path.Ext(name))
	var b strings.Builder
	for _, r := range strings.ToLower(stem) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '.', r == '_', r == '-':
			b.WriteRune(r)
		default:
			b.WriteRune('-')
		}
	}
	cleaned := strings.Trim(b.String(), "-.")
	if cleaned == "" {
		cleaned = "document"
	}
	return cleaned + ext
}

// mapReferenceError puts a rejected upload on 400 and lets everything else
// (disk admission, a missing project, I/O) travel the shared files mapping.
func mapReferenceError(err error) error {
	if errors.Is(err, gitfs.ErrReferenceRejected) {
		return apierr.BadRequest(err.Error())
	}
	return mapFilesError(err)
}
