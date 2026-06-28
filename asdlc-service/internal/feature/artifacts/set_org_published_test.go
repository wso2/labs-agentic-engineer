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

package artifacts

import (
	"context"
	"strings"
	"testing"

	"github.com/wso2/asdlc/asdlc-service/models"
)

// commitRecord captures one CommitDesignFile call.
type commitRecord struct {
	sub     string
	content string
	message string
}

// fakeArtifactSvc embeds ArtifactService so it satisfies the interface while we
// implement only the two methods SetComponentOrgPublished exercises:
// ListDesignFiles (the read) + CommitDesignFile (the commit). Every other method
// panics if reached, proving the durability path takes the commit seam — NOT the
// working-tree-only PutFile path.
type fakeArtifactSvc struct {
	ArtifactService
	files   map[string]string
	commits []commitRecord
}

func (f *fakeArtifactSvc) ListDesignFiles(_ context.Context, _, _ string) (map[string]string, error) {
	return f.files, nil
}

func (f *fakeArtifactSvc) CommitDesignFile(_ context.Context, _, _, sub, content, message string) (string, error) {
	f.commits = append(f.commits, commitRecord{sub: sub, content: content, message: message})
	return "deadbeef", nil
}

// designFilesFor builds a minimal working-tree map for a single service
// component, optionally pre-marked org-published.
func designFilesFor(t *testing.T, name string, alreadyPublished bool) map[string]string {
	t.Helper()
	comp := models.DesignComponent{
		Name:                       name,
		ComponentType:              "service",
		Language:                   "Go",
		AppPath:                    name,
		ComponentAgentInstructions: "serve " + name,
		ExposesAPI:                 &models.ExposesAPI{Managed: true},
	}
	if alreadyPublished {
		comp.ExposesAPI.OrgPublished = true
	}
	files, err := SplitDesign(&DesignFile{Overview: "root", Components: []models.DesignComponent{comp}})
	if err != nil {
		t.Fatalf("SplitDesign: %v", err)
	}
	files[DesignRootFile] = "# Design\n" // ensure ReadDesign sees a non-empty root
	return files
}

func TestSetComponentOrgPublished_CommitsWhenUnset(t *testing.T) {
	svc := &fakeArtifactSvc{files: designFilesFor(t, "employee-api", false)}
	store := NewArtifactStore(svc)

	if err := store.SetComponentOrgPublished(context.Background(), "acme", "hr-directory", "employee-api"); err != nil {
		t.Fatalf("SetComponentOrgPublished: %v", err)
	}
	if len(svc.commits) != 1 {
		t.Fatalf("want exactly one commit, got %d", len(svc.commits))
	}
	c := svc.commits[0]
	if c.sub != "components/employee-api/design.md" {
		t.Fatalf("unexpected committed path %q", c.sub)
	}
	if !strings.Contains(c.content, "orgPublished: true") {
		t.Fatalf("committed design.md missing orgPublished:\n%s", c.content)
	}
	if !strings.Contains(c.message, "org-published") {
		t.Fatalf("unexpected commit message %q", c.message)
	}
}

func TestSetComponentOrgPublished_IdempotentNoCommit(t *testing.T) {
	svc := &fakeArtifactSvc{files: designFilesFor(t, "employee-api", true)}
	store := NewArtifactStore(svc)

	if err := store.SetComponentOrgPublished(context.Background(), "acme", "hr-directory", "employee-api"); err != nil {
		t.Fatalf("SetComponentOrgPublished: %v", err)
	}
	if len(svc.commits) != 0 {
		t.Fatalf("already-published component must not commit, got %d", len(svc.commits))
	}
}

func TestSetComponentOrgPublished_MatchesOCName(t *testing.T) {
	svc := &fakeArtifactSvc{files: designFilesFor(t, "employee-api", false)}
	store := NewArtifactStore(svc)

	// Provider identified by OC component name `<project>-<logical>`.
	if err := store.SetComponentOrgPublished(context.Background(), "acme", "hr-directory", "hr-directory-employee-api"); err != nil {
		t.Fatalf("SetComponentOrgPublished: %v", err)
	}
	if len(svc.commits) != 1 {
		t.Fatalf("OC-name match should commit once, got %d", len(svc.commits))
	}
}

func TestSetComponentOrgPublished_NoMatchNoCommit(t *testing.T) {
	svc := &fakeArtifactSvc{files: designFilesFor(t, "employee-api", false)}
	store := NewArtifactStore(svc)

	if err := store.SetComponentOrgPublished(context.Background(), "acme", "hr-directory", "no-such-component"); err != nil {
		t.Fatalf("SetComponentOrgPublished: %v", err)
	}
	if len(svc.commits) != 0 {
		t.Fatalf("unknown component must not commit, got %d", len(svc.commits))
	}
}
