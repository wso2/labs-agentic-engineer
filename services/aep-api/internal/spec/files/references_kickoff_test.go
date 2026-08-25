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
	"bytes"
	"context"
	"errors"
	"mime/multipart"
	"testing"

	"github.com/wso2/aep/aep-api/internal/gen"
	"github.com/wso2/aep/aep-api/internal/platform/gitfs"
	"github.com/wso2/aep/aep-api/internal/spec"
)

// stubReferenceStore is the FilesService reduced to the one method the
// references upload calls; every other method is unreachable from this path.
type stubReferenceStore struct {
	spec.FilesService
	err   error
	calls int
}

func (s *stubReferenceStore) PutReferences(_ context.Context, _, _ string, _ []gitfs.ReferenceDoc) error {
	s.calls++
	return s.err
}

type recordingKickoff struct {
	calls int
	org   string
	proj  string
}

func (r *recordingKickoff) Kickoff(_ context.Context, orgID, projectID string) {
	r.calls++
	r.org, r.proj = orgID, projectID
}

// oneDocumentBody builds a multipart reader carrying a single reference doc.
func oneDocumentBody(t *testing.T) *multipart.Reader {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	part, err := w.CreateFormFile("files", "brief.md")
	if err != nil {
		t.Fatalf("CreateFormFile: %v", err)
	}
	if _, err := part.Write([]byte("# The brief\n")); err != nil {
		t.Fatalf("write part: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}
	return multipart.NewReader(&buf, w.Boundary())
}

func putReferences(t *testing.T, h *Handler) (gen.PutProjectReferencesResponseObject, error) {
	t.Helper()
	return h.PutProjectReferences(context.Background(), gen.PutProjectReferencesRequestObject{
		ProjectName: "expense-approval",
		Body:        oneDocumentBody(t),
	})
}

// The upload is the SECOND kickoff trigger (#562): a create that declared
// documents were coming held its own, because the documents are the primary
// brief and an interview started before they land is conducted blind.
func TestPutProjectReferences_ReleasesTheHeldKickoff(t *testing.T) {
	k := &recordingKickoff{}
	h := New(&stubReferenceStore{}, nil).WithKickoffStarter(k)

	if _, err := putReferences(t, h); err != nil {
		t.Fatalf("put references: %v", err)
	}
	if k.calls != 1 {
		t.Fatalf("kickoffs = %d, want 1", k.calls)
	}
	if k.proj != "expense-approval" {
		t.Fatalf("kickoff project = %q, want expense-approval", k.proj)
	}
}

// Nothing was stored, so nothing is in front of the agent — a kickoff here
// would interview against documents the upload never accepted.
func TestPutProjectReferences_NoKickoffWhenTheStoreRejects(t *testing.T) {
	k := &recordingKickoff{}
	h := New(&stubReferenceStore{err: errors.New("disk is full")}, nil).WithKickoffStarter(k)

	if _, err := putReferences(t, h); err == nil {
		t.Fatal("a rejected upload returned no error")
	}
	if k.calls != 0 {
		t.Fatalf("kickoffs = %d, want 0 when nothing was stored", k.calls)
	}
}

// An unwired starter is a documented no-op: the upload still stores.
func TestPutProjectReferences_NilKickoffStarterIsNoOp(t *testing.T) {
	store := &stubReferenceStore{}
	h := New(store, nil)

	if _, err := putReferences(t, h); err != nil {
		t.Fatalf("put references: %v", err)
	}
	if store.calls != 1 {
		t.Fatalf("stores = %d, want 1", store.calls)
	}
}
