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
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"path"
	"strings"

	"github.com/wso2/aep/ae-studio-tools/internal/gen"
	"github.com/wso2/aep/ae-studio-tools/internal/repo"
)

// The references upload (09 §1): aep-api relays the documents a user attached
// on the create view, and the pod owns every check on them (moved from
// aep-api's spec/files/references.go). The validator skips this op's
// multipart body, so the parts are read here one at a time, each bounded,
// and the request is never held in memory whole.

// ReferenceStore keeps a repository's reference documents (repo.Engine).
type ReferenceStore interface {
	PutReferences(ctx context.Context, r repo.OwnerRepo, docs []repo.ReferenceDoc) error
}

// referencesField is the multipart field repeated once per document.
const referencesField = "files"

// errMalformedUpload is a body that is not the contract's multipart shape:
// unreadable framing or a field other than files.
var errMalformedUpload = errors.New("malformed references upload")

// PutRepoReferences replaces the repository's stored set with the uploaded
// documents. The owner guard (ownerGuard) already refused an owner that is
// not the org's connected GitHub account before any part was read: the path
// is the store key, and the store itself does not know which owners the org
// may use.
func (s internalServer) PutRepoReferences(ctx context.Context, request gen.PutRepoReferencesRequestObject) (gen.PutRepoReferencesResponseObject, error) {
	store := repo.OwnerRepo{Owner: request.Owner, Repo: request.Repo}
	docs, err := readReferenceParts(request.Body)
	if err == nil {
		err = s.refs.PutReferences(ctx, store, docs)
	}
	if err != nil {
		return referencesProblem(ctx, store, err)
	}
	return gen.PutRepoReferences204Response{}, nil
}

// referencesProblem maps an upload failure to its answer. Anything not listed
// is the generic 500 (writeResponseError logs it).
func referencesProblem(ctx context.Context, store repo.OwnerRepo, err error) (gen.PutRepoReferencesResponseObject, error) {
	var maxErr *http.MaxBytesError
	switch {
	case errors.Is(err, repo.ErrReferenceRejected):
		return gen.PutRepoReferences400ApplicationProblemPlusJSONResponse(
			newProblem(http.StatusBadRequest, "reference_rejected", err.Error())), nil
	case errors.Is(err, errMalformedUpload):
		return gen.PutRepoReferences400ApplicationProblemPlusJSONResponse(
			newProblem(http.StatusBadRequest, "validation_failed", "the request does not match the contract")), nil
	case errors.As(err, &maxErr):
		return gen.PutRepoReferences413ApplicationProblemPlusJSONResponse(
			newProblem(http.StatusRequestEntityTooLarge, "payload_too_large", "the request body exceeds the size limit")), nil
	case errors.Is(err, repo.ErrDiskFull):
		slog.WarnContext(ctx, "files.disk_full", "op", "put-references", "repo", strings.ToLower(store.Owner+"/"+store.Repo))
		return gen.PutRepoReferences503ApplicationProblemPlusJSONResponse(
			newProblem(http.StatusServiceUnavailable, "disk_full", "the studio's disk is full")), nil
	default:
		return nil, err
	}
}

// readReferenceParts reads the upload part by part. Each document is read to
// one byte past repo.MaxReferenceBytes: io.LimitReader ends a capped read
// with io.EOF, indistinguishable from a small file ending, and storing a
// truncated PDF is worse than refusing it. An eleventh document is refused
// before it is read. The engine checks the set again (names, types, sizes,
// duplicates) and is the authority.
func readReferenceParts(body *multipart.Reader) ([]repo.ReferenceDoc, error) {
	var docs []repo.ReferenceDoc
	// Sanitizing can map two different uploads onto one stored name ("My
	// Notes.md" and "my-notes.md"). Naming both here tells the caller which
	// two collided; the engine's "duplicate name" alone would not.
	byStoredName := map[string]string{}
	for {
		part, err := body.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, uploadReadError(err)
		}
		if part.FormName() != referencesField {
			return nil, fmt.Errorf("%w: unexpected field %q", errMalformedUpload, part.FormName())
		}
		if len(docs) >= repo.MaxReferenceCount {
			return nil, fmt.Errorf("%w: at most %d documents per repository", repo.ErrReferenceRejected, repo.MaxReferenceCount)
		}
		name := sanitizeReferenceName(part.FileName())
		if prior, ok := byStoredName[name]; ok {
			return nil, fmt.Errorf("%w: %q and %q both become %q, rename one", repo.ErrReferenceRejected, prior, part.FileName(), name)
		}
		byStoredName[name] = part.FileName()
		content, err := io.ReadAll(io.LimitReader(part, repo.MaxReferenceBytes+1))
		if err != nil {
			return nil, uploadReadError(err)
		}
		if len(content) > repo.MaxReferenceBytes {
			return nil, fmt.Errorf("%w: %q exceeds the %d MiB per-document limit", repo.ErrReferenceRejected, name, repo.MaxReferenceBytes>>20)
		}
		docs = append(docs, repo.ReferenceDoc{Name: name, Content: content})
	}
	if len(docs) == 0 {
		return nil, fmt.Errorf("%w: no documents in the upload", repo.ErrReferenceRejected)
	}
	return docs, nil
}

// uploadReadError keeps the body cap's error (413) and calls any other read
// failure a malformed upload (400).
func uploadReadError(err error) error {
	var maxErr *http.MaxBytesError
	if errors.As(err, &maxErr) {
		return err
	}
	return fmt.Errorf("%w: %v", errMalformedUpload, err)
}

// sanitizeReferenceName reduces a client-supplied file name to a bare,
// store-safe name. The field is caller-controlled: path.Base strips any
// directory a crafted part carries (a Windows-style one too, once its
// backslashes are slashes), and the stem loses everything outside the
// store's alphabet. The engine validates the result again; this only keeps a
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
