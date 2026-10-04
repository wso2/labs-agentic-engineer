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

package provisioning

import (
	"context"
	"errors"
	"testing"

	"github.com/wso2/aep/aep-api/internal/clients/aestudiotools/aestudiotest"
	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
)

// docsRepoService answers EnsureBareRepo with the org's resource-docs row;
// nothing else of RepoService is used by the read.
type docsRepoService struct {
	sourcecontrol.RepoService
	url string
}

func (s docsRepoService) EnsureBareRepo(_ context.Context, orgID, projectID, _ string) (*sourcecontrol.GitRepository, error) {
	return &sourcecontrol.GitRepository{OrgID: orgID, ProjectID: projectID, RepoURL: s.url, Status: "ready"}, nil
}

// TestOrgResourceDocs_ReadUTF8ThroughThePod: the read goes to the org's pod
// at the docs repository's tip, and a missing document is an error naming it.
func TestOrgResourceDocs_ReadUTF8ThroughThePod(t *testing.T) {
	ctx := context.Background()
	f := aestudiotest.New()
	ref := sourcecontrol.RepoRef{Org: "default", Owner: "acme", Repo: "org-resource-docs"}
	f.SeedRepo(ref, map[string]string{"payments/openapi.yaml": "openapi: 3.1.0"})
	docs := NewGitOrgResourceDocs(docsRepoService{url: "https://github.com/acme/org-resource-docs"}, f)

	got, err := docs.ReadUTF8(ctx, "default", "payments/openapi.yaml")
	if err != nil || got != "openapi: 3.1.0" {
		t.Fatalf("ReadUTF8 = (%q, %v)", got, err)
	}
	calls := f.Calls()
	if len(calls) != 1 || calls[0].Op != aestudiotest.OpReadFile || calls[0].Ref.Repo != "org-resource-docs" || calls[0].At != "" {
		t.Fatalf("calls = %+v, want one read-file of org-resource-docs at the tip", calls)
	}
	if _, err := docs.ReadUTF8(ctx, "default", "payments/missing.yaml"); !errors.Is(err, sourcecontrol.ErrPathNotFound) {
		t.Fatalf("missing doc: err = %v, want ErrPathNotFound", err)
	}
}

// TestOrgResourceDocs_CommitReplacesTheDocument: a commit lands the document
// at <logicalName>/<fileName>, over whatever the tip held there.
func TestOrgResourceDocs_CommitReplacesTheDocument(t *testing.T) {
	ctx := context.Background()
	f := aestudiotest.New()
	ref := sourcecontrol.RepoRef{Org: "default", Owner: "acme", Repo: "org-resource-docs"}
	f.SeedRepo(ref, map[string]string{"payments/openapi.yaml": "openapi: 3.0.0"})
	docs := NewGitOrgResourceDocs(docsRepoService{url: "https://github.com/acme/org-resource-docs"}, f)

	path, err := docs.CommitUTF8(ctx, "default", "payments", "openapi.yaml", "openapi: 3.1.0")
	if err != nil || path != "payments/openapi.yaml" {
		t.Fatalf("CommitUTF8 = (%q, %v)", path, err)
	}
	got, _, _ := f.ReadFile(ctx, ref, "", path)
	if string(got) != "openapi: 3.1.0" {
		t.Fatalf("tip holds %q, want the new document", got)
	}
}

// TestOrgResourceDocs_CommitRetriesOnConflict: a writer that moves the path
// between the read and the commit makes the commit conflict; the store
// re-reads and lands its document on the next attempt.
func TestOrgResourceDocs_CommitRetriesOnConflict(t *testing.T) {
	ctx := context.Background()
	f := aestudiotest.New()
	ref := sourcecontrol.RepoRef{Org: "default", Owner: "acme", Repo: "org-resource-docs"}
	f.SeedRepo(ref, map[string]string{})
	raced := false
	f.BeforeCommit(func() {
		if raced {
			return
		}
		raced = true
		_, _ = f.Commit(ctx, ref, sourcecontrol.CommitRequest{Writes: []sourcecontrol.FileWrite{{Path: "payments/openapi.yaml", Content: "theirs"}}})
	})
	docs := NewGitOrgResourceDocs(docsRepoService{url: "https://github.com/acme/org-resource-docs"}, f)

	if _, err := docs.CommitUTF8(ctx, "default", "payments", "openapi.yaml", "ours"); err != nil {
		t.Fatalf("CommitUTF8: %v", err)
	}
	got, _, _ := f.ReadFile(ctx, ref, "", "payments/openapi.yaml")
	if string(got) != "ours" {
		t.Fatalf("tip holds %q, want ours after the retry", got)
	}
}
